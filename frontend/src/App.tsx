import { useEffect, useState } from "react";
import Welcome from "./windows/Welcome";
import Workspace from "./windows/Workspace";
import { KeyboardShortcutsProvider } from "./state/keybinding";

function App() {
  const [hash, setHash] = useState(() => window.location.hash.replace("#", ""));
  useEffect(() => {
    const onHashChange = () => setHash(window.location.hash.replace("#", ""));
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);
  if (hash === "welcome") {
    return <Welcome />;
  }
  return (
    <KeyboardShortcutsProvider>
      <Workspace />
    </KeyboardShortcutsProvider>
  );
}

export default App;
