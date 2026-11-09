import { useState } from "react";
import { mesh, messageOf } from "../api";
import type { ConnectionStatus, NetworkView } from "../api";
import { Card, Switch } from "../components";
import type { Notify } from "../components";

type Props = {
  status: ConnectionStatus | null;
  network: NetworkView | null;
  busy: boolean;
  onRefresh: (quiet?: boolean) => Promise<void> | void;
  onNotify: Notify;
  onGoSettings: () => void;
};

export default function HomeView({ status, network, busy, onRefresh, onNotify, onGoSettings }: Props) {
  const [pending, setPending] = useState(false);

  const configured = status?.configured ?? false;
  const running = status?.running ?? false;
  const accept = network?.identity.accept ?? false;

  const toggleConnection = async () => {
    setPending(true);
    try {
      if (running) {
        await mesh.disconnect();
        onNotify("ok", "VPN", "Соединение разорвано");
      } else {
        await mesh.connect();
        onNotify("ok", "VPN", "Соединение установлено");
      }
      await onRefresh(false);
    } catch (error) {
      onNotify("error", "VPN", messageOf(error));
    } finally {
      setPending(false);
    }
  };

  const toggleAccept = async (next: boolean) => {
    try {
      await mesh.setAccept(next);
      await onRefresh(true);
    } catch (error) {
      onNotify("error", "Доступ", messageOf(error));
    }
  };

  return (
    <>
      <section className="section">
        <div className="section-head">
          <h2>Подключение VPN</h2>
          <span className="spacer" />
          <span className={running ? "badge is-on" : "badge is-off"}>
            {running ? "Активно" : "Неактивно"}
          </span>
        </div>

        {configured ? (
          <Card>
            <div className="card-body">
              <div className="row">
                <span className="label">Сеть</span>
                <span className="value">{status?.network || "—"}</span>
              </div>
              <div className="row">
                <span className="label">VPN-адрес</span>
                <span className="value mono">{status?.address || "—"}</span>
              </div>
              <div className="row">
                <span className="label">Соединение</span>
                <span className="value">{running ? "Прямое" : "—"}</span>
              </div>
            </div>
            <div className="card-foot">
              <button
                className={running ? "btn" : "btn btn-primary"}
                disabled={pending || busy || !status?.serviceUp}
                onClick={() => void toggleConnection()}
              >
                {running ? "Отключить" : "Подключить"}
              </button>
            </div>
          </Card>
        ) : (
          <Card>
            <div className="empty">Импортируйте конфигурацию WireGuard, чтобы войти в сеть Alcea.</div>
            <div className="card-foot">
              <button className="btn btn-primary" onClick={onGoSettings}>
                Импортировать конфигурацию
              </button>
            </div>
          </Card>
        )}

        {status && !status.serviceUp && (
          <div className="error">
            Служба AlceaMesh не запущена. Запустите <code>alceameshd</code>, чтобы управлять туннелем.
          </div>
        )}
      </section>

      <section className="section">
        <div className="section-head">
          <h2>Обмен</h2>
        </div>
        <Card>
          <div className="switch-row">
            <div className="text">
              <span className="title">Принимать файлы</span>
              <span className="hint">Другие увидят вас в списке</span>
            </div>
            <Switch
              checked={accept}
              disabled={!configured || busy}
              onToggle={(next) => void toggleAccept(next)}
            />
          </div>
        </Card>
      </section>
    </>
  );
}
