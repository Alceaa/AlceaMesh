import { useEffect, useState } from "react";
import { mesh, messageOf } from "../api";
import type { ConnectionStatus, NetworkTransport, NetworkView, ServiceState, UpdateState } from "../api";
import { Card, Switch } from "../components";
import type { Notify } from "../components";

type Props = {
  status: ConnectionStatus | null;
  network: NetworkView | null;
  transport: NetworkTransport | null;
  busy: boolean;
  onRefresh: (quiet?: boolean) => Promise<void> | void;
  onNotify: Notify;
  onGoSettings: () => void;
};

export default function HomeView({
  status,
  network,
  transport,
  busy,
  onRefresh,
  onNotify,
  onGoSettings,
}: Props) {
  const [pending, setPending] = useState(false);
  const [service, setService] = useState<ServiceState | null>(null);
  const [update, setUpdate] = useState<UpdateState | null>(null);
  const [dismissed, setDismissed] = useState("");
  const [installing, setInstalling] = useState(false);

  const configured = status?.configured ?? false;
  const running = status?.running ?? false;
  const serviceUp = status?.serviceUp ?? false;
  const accept = network?.identity.accept ?? false;
  const direct = transport?.direct ?? false;
  const connectionLabel = !running ? "—" : direct ? "Прямое" : "Через сервер";

  const startService = async (asAdmin: boolean) => {
    try {
      const result = await mesh.startService(asAdmin);
      setService(result);
      await onRefresh(true);
    } catch (error) {
      onNotify("error", "VPN", messageOf(error));
    }
  };

  const startInstall = async () => {
    setInstalling(true);
    try {
      const message = await mesh.installUpdate();
      onNotify("ok", "Обновление", message);
    } catch (error) {
      setInstalling(false);
      setUpdate((current) => (current ? { ...current, auto: false } : current));
      onNotify("error", "Обновление", messageOf(error));
    }
  };

  useEffect(() => {
    if (!update || !update.available || !update.auto) return;
    void startInstall();
  }, [update]);

  useEffect(() => {
    if (serviceUp) {
      setService(null);
      return;
    }
    let cancelled = false;
    void (async () => {
      try {
        const result = await mesh.startService(false);
        if (!cancelled) setService(result);
      } catch {
        if (!cancelled) setService(null);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [serviceUp]);

  useEffect(() => {
    let cancelled = false;
    const check = async () => {
      try {
        const state = await mesh.updateCheck();
        if (cancelled) return;
        setUpdate(state.available ? state : null);
      } catch {
        if (!cancelled) setUpdate(null);
      }
    };
    const timer = window.setTimeout(() => void check(), 15000);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, []);

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
      {update && update.available && update.latest !== dismissed ? (
        <section className="section">
          <div className="section-head">
            <h2>Доступно обновление</h2>
            <span className="spacer" />
            <span className="badge is-on">{update.latest}</span>
          </div>
          <Card>
            <div className="card-body">
              <div className="row">
                <span className="label">Установлено</span>
                <span className="value mono">{update.current}</span>
              </div>
              {update.notes ? (
                <div className="row">
                  <span className="label">Что нового</span>
                  <span className="value">{update.notes}</span>
                </div>
              ) : null}
            </div>
            <div className="card-foot">
              <button className="btn btn-primary" disabled={installing} onClick={() => void startInstall()}>
                Обновить сейчас
              </button>
              <button className="btn" disabled={installing} onClick={() => setDismissed(update.latest)}>
                Позже
              </button>
            </div>
            {installing ? (
              <div className="hint">
                Скачиваю обновление и запускаю установщик: приложение закроется и откроется заново.
              </div>
            ) : null}
          </Card>
        </section>
      ) : null}

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
                <span className="value">{connectionLabel}</span>
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
            <div className="empty">Импортируйте конфигурацию, чтобы войти в сеть Alcea.</div>
            <div className="card-foot">
              <button className="btn btn-primary" onClick={onGoSettings}>
                Импортировать конфигурацию
              </button>
            </div>
          </Card>
        )}

        {status && !status.serviceUp && (
          <div className="error">
            <div>
              {service?.message ||
                "VPN недоступен: служба AlceaMesh не отвечает. Перезапустите приложение или запустите службу от имени администратора."}
            </div>
            {service?.needsRights ? (
              <div className="row" style={{ marginTop: 10 }}>
                <button className="btn" onClick={() => void startService(true)}>
                  Запустить службу с правами администратора
                </button>
              </div>
            ) : null}
            {service && !service.installed ? (
              <div className="row" style={{ marginTop: 10 }}>
                <button className="btn" onClick={() => void startService(false)}>
                  Проверить службу ещё раз
                </button>
              </div>
            ) : null}
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
