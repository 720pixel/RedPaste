// Ties a Yjs document to the encrypted WebSocket relay. Local edits are encrypted
// and pushed; remote envelopes are decrypted and applied. Awareness (cursors) goes
// over the same socket but is never persisted.

import * as Y from "yjs";
import { Awareness, encodeAwarenessUpdate, applyAwarenessUpdate, removeAwarenessStates } from "y-protocols/awareness";
import type { RoomKeys } from "./roomcrypto";
import { encryptUpdate, decryptUpdate } from "./roomcrypto";
import { bootstrap, saveSnapshot, type Bootstrap } from "./api";
import { fromB64url } from "../crypto/base64";

export type ConnStatus = "connecting" | "connected" | "reconnecting" | "closed";

interface Options {
  slug: string;
  keys: RoomKeys;
  doc: Y.Doc;
  awareness: Awareness;
  onStatus: (s: ConnStatus) => void;
  onPeers: (n: number) => void;
  onFrozen: () => void;
  onGone: (reason: string) => void;
  initial?: Bootstrap; // already-fetched bootstrap, used once to skip a re-fetch
}

const SNAPSHOT_EVERY = 100;
const SNAPSHOT_INTERVAL_MS = 60_000;

export class LiveProvider {
  private ws: WebSocket | null = null;
  private o: Options;
  private authed = false;
  private queue: string[] = []; // update envelopes received before bootstrap finished
  private ready = false;
  private maxSeq = 0;
  private updatesSinceSnapshot = 0;
  private lastSnapshot = 0;
  private snapshotting = false;
  private retry = 0;
  private closed = false;
  private readonly origin = {}; // marks locally-applied updates so we don't echo them

  constructor(o: Options) {
    this.o = o;
    this.o.doc.on("update", this.onDocUpdate);
    this.o.awareness.on("update", this.onAwarenessUpdate);
    this.connect();
  }

  destroy() {
    this.closed = true;
    this.o.doc.off("update", this.onDocUpdate);
    this.o.awareness.off("update", this.onAwarenessUpdate);
    removeAwarenessStates(this.o.awareness, [this.o.doc.clientID], "destroy");
    this.ws?.close();
  }

  private connect() {
    if (this.closed) return;
    this.o.onStatus(this.retry === 0 ? "connecting" : "reconnecting");
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${proto}//${location.host}/api/v1/live/${encodeURIComponent(this.o.slug)}/signal`);
    this.ws = ws;
    ws.onopen = () => {
      // first frame authenticates (TCP keeps it ahead of any edits we send next);
      // the editor token is derived, never the room key
      ws.send(JSON.stringify({ t: "auth", token: this.o.keys.editorToken }));
      this.authed = true;
      this.retry = 0;
      this.o.onStatus("connected");
      // bootstrap is a separate authenticated HTTP call; if the token is bad it
      // fails there and we stop.
      void this.doBootstrap();
    };
    ws.onmessage = (ev) => this.onMessage(ev.data);
    ws.onclose = () => this.onClose();
    ws.onerror = () => ws.close();
  }

  private async onMessage(raw: string) {
    let msg: { t: string; data?: string; count?: number; seq?: number; code?: string };
    try {
      msg = JSON.parse(raw);
    } catch {
      return;
    }
    switch (msg.t) {
      case "update":
        if (msg.data) {
          if (typeof msg.seq === "number") this.maxSeq = Math.max(this.maxSeq, msg.seq);
          if (this.ready) await this.applyUpdate(msg.data);
          else this.queue.push(msg.data);
        }
        break;
      case "awareness":
        if (msg.data) await this.applyAwareness(msg.data);
        break;
      case "peers":
        if (typeof msg.count === "number") this.o.onPeers(msg.count);
        break;
      case "error":
        if (msg.code === "frozen") this.o.onFrozen();
        else if (msg.code === "room_full") this.o.onGone("This room is full.");
        else if (msg.code === "storage_full") this.o.onGone("This room has reached its storage limit.");
        break;
    }
  }

  private initialUsed = false;

  private async doBootstrap() {
    try {
      // Use the pre-fetched bootstrap on the first connect; refetch on reconnects.
      let boot: Bootstrap;
      if (this.o.initial && !this.initialUsed) {
        boot = this.o.initial;
        this.initialUsed = true;
      } else {
        boot = await bootstrap(this.o.slug, this.o.keys.editorToken);
      }
      if (boot.snapshot) {
        this.maxSeq = Math.max(this.maxSeq, boot.snapshot.seq);
        await this.applyUpdate(boot.snapshot.ciphertext);
      }
      for (const u of boot.updates) {
        this.maxSeq = Math.max(this.maxSeq, u.seq);
        await this.applyUpdate(u.ciphertext);
      }
      // flush anything that arrived while bootstrapping
      for (const env of this.queue) await this.applyUpdate(env);
      this.queue = [];
      this.ready = true;
      this.lastSnapshot = Date.now();
      if (boot.state === "frozen") this.o.onFrozen();
    } catch {
      // bad capability or gone room; stop, don't spin in a reconnect loop
      this.closed = true;
      this.ws?.close();
      this.o.onGone("This room is unavailable.");
    }
  }

  private applyUpdate = async (envB64: string) => {
    try {
      const update = await decryptUpdate(this.o.keys.roomKey, envB64);
      Y.applyUpdate(this.o.doc, update, this.origin);
    } catch {
      /* drop undecryptable / malformed updates */
    }
  };

  private applyAwareness = async (envB64: string) => {
    try {
      const update = await decryptUpdate(this.o.keys.roomKey, envB64);
      applyAwarenessUpdate(this.o.awareness, update, this.origin);
    } catch {
      /* ignore */
    }
  };

  private onDocUpdate = async (update: Uint8Array, origin: unknown) => {
    if (origin === this.origin) return; // came from the network, don't echo
    if (!this.authed || this.ws?.readyState !== WebSocket.OPEN) return;
    const env = await encryptUpdate(this.o.keys.roomKey, update);
    this.ws.send(JSON.stringify({ t: "update", data: env }));
    this.updatesSinceSnapshot++;
    this.maybeSnapshot();
  };

  private onAwarenessUpdate = async (
    { added, updated, removed }: { added: number[]; updated: number[]; removed: number[] },
    origin: unknown,
  ) => {
    if (origin === this.origin) return;
    if (!this.authed || this.ws?.readyState !== WebSocket.OPEN) return;
    const changed = added.concat(updated, removed);
    const env = await encryptUpdate(this.o.keys.roomKey, encodeAwarenessUpdate(this.o.awareness, changed));
    this.ws.send(JSON.stringify({ t: "awareness", data: env }));
  };

  // one client periodically compacts by uploading an encrypted snapshot
  private async maybeSnapshot() {
    if (this.snapshotting || !this.ready) return;
    const due = this.updatesSinceSnapshot >= SNAPSHOT_EVERY || Date.now() - this.lastSnapshot >= SNAPSHOT_INTERVAL_MS;
    if (!due) return;
    this.snapshotting = true;
    try {
      const state = Y.encodeStateAsUpdate(this.o.doc);
      const env = await encryptUpdate(this.o.keys.roomKey, state);
      // envelope is base64url; the API wants bytes, so hand back the decoded bytes
      await saveSnapshot(this.o.slug, this.o.keys.editorToken, fromB64url(env), this.maxSeq);
      this.updatesSinceSnapshot = 0;
      this.lastSnapshot = Date.now();
    } catch {
      /* a failed snapshot is fine; we'll try again */
    } finally {
      this.snapshotting = false;
    }
  }

  private onClose() {
    this.authed = false;
    this.ready = false;
    if (this.closed) {
      this.o.onStatus("closed");
      return;
    }
    // reconnect with capped backoff; a fresh bootstrap catches us up
    this.retry++;
    const delay = Math.min(1000 * 2 ** this.retry, 15_000);
    this.o.onStatus("reconnecting");
    setTimeout(() => this.connect(), delay);
  }
}
