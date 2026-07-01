import { createRoot } from "react-dom/client";
import { App } from "./app/App";
import "./index.css";

// No StrictMode: it double-invokes effects in dev, which would double-mount
// CodeMirror and the live provider.
const el = document.getElementById("app");
if (el) createRoot(el).render(<App />);
