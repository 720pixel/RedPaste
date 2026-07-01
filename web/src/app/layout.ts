// Lets a route ask the shell for the full-width main content area instead of
// the standard centered column, for screens (like the diff workspace) that
// need real estate more than a marketing-page reading width.

import { createContext, useContext, useEffect } from "react";

export const WideLayoutContext = createContext<(wide: boolean) => void>(() => {});

export function useWideLayout(wide: boolean): void {
  const setWide = useContext(WideLayoutContext);
  useEffect(() => {
    setWide(wide);
    return () => setWide(false);
  }, [wide, setWide]);
}
