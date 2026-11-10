import { useCallback, useEffect, useRef, useState } from "react";
import { mesh, messageOf } from "./api";
import type { ConnectionStatus, NetworkTransport, NetworkView } from "./api";
import { Pill } from "./components";
import { GearIcon, LogoIcon, MonitorIcon, SendIcon } from "./icons";
import HomeView from "./views/Home";
import SettingsView from "./views/Settings";
import TransferView from "./views/Transfer";

type Tab = "vpn" | "transfer" | "settings";
type Toast = { id: number; kind: "ok" | "error"; title: string; message: string };

export default function App() {
  const [tab, setTab] = useState<Tab>("vpn");
  const [status, setStatus] = useState<ConnectionStatus | null>(null);
  const [network, setNetwork] = useState<NetworkView | null>(null);
  const [transport, setTransport] = useState<NetworkTransport | null>(null);
  const [busy, setBusy] = useState(false);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const toastID = useRef(0);

  const notify = useCallback((kind: Toast["kind"], title: string, message: string) => {
    const id = ++toastID.current;
    setToasts((list) => [...list, { id, kind, title, message }]);
    window.setTimeout(() => setToasts((list) => list.filter((toast) => toast.id !== id)), 4500);
  }, []);

  const refresh = useCallback(
    async (quiet = false) => {
      if (!quiet) setBusy(true);
      try {
        const [connection, peers, link] = await Promise.all([
          mesh.status(),
          mesh.network(),
          mesh.transport(),
        ]);
        setStatus(connection);
        setNetwork(peers);
        setTransport(link);
      } catch (error) {
        if (!quiet) notify("error", "Ошибка", messageOf(error));
      } finally {
        if (!quiet) setBusy(false);
      }
    },
    [notify],
  );

  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => void refresh(true), 5000);
    return () => window.clearInterval(timer);
  }, [refresh]);

  const connected = status?.running ?? false;

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <span className="brand-mark">
            <LogoIcon />
          </span>
          <span className="brand-name">Alcea Network</span>
        </div>
        <span className="spacer" />
        <Pill label={connected ? "Подключено" : "Отключено"} on={connected} />
      </header>

      <div className="content">
        {tab === "vpn" && (
          <HomeView
            status={status}
            network={network}
            transport={transport}
            busy={busy}
            onRefresh={refresh}
            onNotify={notify}
            onGoSettings={() => setTab("settings")}
          />
        )}
        {tab === "transfer" && (
          <TransferView network={network} transport={transport} onRefresh={refresh} onNotify={notify} />
        )}
        {tab === "settings" && (
          <SettingsView status={status} network={network} onRefresh={refresh} onNotify={notify} />
        )}
      </div>

      <nav className="nav">
        <button className={tab === "vpn" ? "is-active" : ""} onClick={() => setTab("vpn")}>
          <MonitorIcon />
          VPN
        </button>
        <button className={tab === "transfer" ? "is-active" : ""} onClick={() => setTab("transfer")}>
          <SendIcon size={20} />
          Передача
        </button>
        <button className={tab === "settings" ? "is-active" : ""} onClick={() => setTab("settings")}>
          <GearIcon size={20} />
          Настройки
        </button>
      </nav>

      <div className="toasts">
        {toasts.map((toast) => (
          <div key={toast.id} className={toast.kind === "ok" ? "toast is-ok" : "toast is-error"}>
            <span className="toast-title">{toast.title}</span>
            <span>{toast.message}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
