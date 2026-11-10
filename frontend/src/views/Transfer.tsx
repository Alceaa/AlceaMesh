import { useState } from "react";
import { mesh, messageOf } from "../api";
import type { DeviceView, NetworkTransport, NetworkView, TransferProgress } from "../api";
import { Card } from "../components";
import type { Notify } from "../components";
import { SendIcon } from "../icons";

type Props = {
  network: NetworkView | null;
  transport: NetworkTransport | null;
  onRefresh: (quiet?: boolean) => Promise<void> | void;
  onNotify: Notify;
};

const STATE_LABEL: Record<string, string> = {
  probing: "Поиск пути…",
  direct: "Прямое",
  relay: "Через сервер",
};

function formatBytes(value: number): string {
  if (value < 1024) {
    return `${value} Б`;
  }
  if (value < 1024 * 1024) {
    return `${(value / 1024).toFixed(1)} КБ`;
  }
  if (value < 1024 * 1024 * 1024) {
    return `${(value / (1024 * 1024)).toFixed(1)} МБ`;
  }
  return `${(value / (1024 * 1024 * 1024)).toFixed(2)} ГБ`;
}

function percentOf(progress: TransferProgress): number {
  if (progress.total <= 0) {
    return 0;
  }
  return Math.max(0, Math.min(100, Math.round((progress.sent / progress.total) * 100)));
}

export default function TransferView({ network, transport, onRefresh, onNotify }: Props) {
  const [pending, setPending] = useState("");

  const others = (network?.devices ?? []).filter((device) => !device.self);
  const transfers = transport?.transfers ?? [];

  const statusOf = (peerKey: string) => (transport?.peers ?? []).find((item) => item.peer === peerKey);

  const connect = async (device: DeviceView) => {
    setPending(device.publicKey);
    try {
      await mesh.connectPeer(device.publicKey, device.name, device.meshIP);
      onNotify("ok", "Прямое соединение", `Устанавливаем прямое соединение с «${device.name}»`);
      await onRefresh(true);
    } catch (error) {
      onNotify("error", "Прямое соединение", messageOf(error));
    } finally {
      setPending("");
    }
  };

  const disconnect = async (device: DeviceView) => {
    try {
      await mesh.disconnectPeer(device.publicKey);
      await onRefresh(true);
    } catch (error) {
      onNotify("error", "Прямое соединение", messageOf(error));
    }
  };

  const send = async (device: DeviceView) => {
    setPending(device.publicKey);
    try {
      const result = await mesh.sendFile(device.publicKey);
      if (!result.name) {
        return;
      }
      if (result.error) {
        onNotify("error", "Передача", result.error);
      } else {
        onNotify("ok", "Передача", `«${result.name}» отправлен`);
      }
      await onRefresh(true);
    } catch (error) {
      onNotify("error", "Передача", messageOf(error));
    } finally {
      setPending("");
    }
  };

  return (
    <>
      <section className="section">
        <div className="section-head">
          <h2>Отправка файла</h2>
        </div>
        <Card>
          <div className="empty">
            Установите прямое соединение и нажмите «Отправить». Принятые файлы складываются в папку
            {transport?.inbox ? ` ${transport.inbox}` : " загрузок"}.
          </div>
        </Card>
      </section>

      <section className="section">
        <div className="section-head">
          <h2>Доступные устройства</h2>
          {transport?.publicEndpoint ? (
            <>
              <span className="spacer" />
              <span className="hint mono">{transport.publicEndpoint}</span>
            </>
          ) : null}
        </div>
        <Card>
          {others.length > 0 ? (
            <div className="devices">
              {others.map((device) => {
                const status = statusOf(device.publicKey);
                const state = status?.state ?? "idle";
                const details = [device.meshIP || "—"];
                if (state === "direct") {
                  if (status?.endpoint) {
                    details.push(status.endpoint);
                  }
                  if (status?.rttMs) {
                    details.push(`${status.rttMs} мс`);
                  }
                }
                return (
                  <div className="device" key={device.id}>
                    <span className="device-icon">
                      <SendIcon size={20} />
                    </span>
                    <div className="device-main">
                      <div className="device-name">{device.name}</div>
                      <div className="device-sub">{details.join(" · ")}</div>
                    </div>
                    <span className="device-actions">
                      {state === "direct" ? (
                        <>
                          <button
                            className="btn chip btn-primary"
                            disabled={pending === device.publicKey}
                            onClick={() => void send(device)}
                          >
                            Отправить
                          </button>
                          <button className="badge is-on" onClick={() => void disconnect(device)}>
                            Прямое
                          </button>
                        </>
                      ) : state === "idle" ? (
                        <button
                          className="btn chip"
                          disabled={pending === device.publicKey || !device.publicKey}
                          onClick={() => void connect(device)}
                        >
                          Соединить
                        </button>
                      ) : (
                        <button className="badge" onClick={() => void disconnect(device)}>
                          {STATE_LABEL[state] ?? state}
                        </button>
                      )}
                    </span>
                  </div>
                );
              })}
            </div>
          ) : (
            <div className="empty">Сейчас нет устройств, готовых принять файл.</div>
          )}
        </Card>
        {transport?.message ? <div className="hint">{transport.message}</div> : null}
      </section>

      {transfers.length > 0 ? (
        <section className="section">
          <div className="section-head">
            <h2>Передачи</h2>
          </div>
          <Card>
            <div className="devices">
              {transfers.map((progress) => (
                <div
                  className="transfer"
                  key={`${progress.peer}-${progress.direction}-${progress.name}`}
                >
                  <div className="transfer-head">
                    <span className="transfer-name">{progress.name || "файл"}</span>
                    <span className="hint">
                      {progress.direction === "send" ? "отправка" : "приём"}
                      {progress.resumed ? " · докачка" : ""}
                    </span>
                    <span className="spacer" />
                    <span className="hint mono">
                      {formatBytes(progress.sent)} / {formatBytes(progress.total)}
                    </span>
                  </div>
                  <div className="progress">
                    <div
                      className="progress-bar"
                      data-done={progress.done && !progress.error ? "1" : "0"}
                      style={{ width: `${percentOf(progress)}%` }}
                    />
                  </div>
                  {progress.error ? (
                    <div className="hint transfer-error">{progress.error}</div>
                  ) : progress.done ? (
                    <div className="hint">готово</div>
                  ) : null}
                </div>
              ))}
            </div>
          </Card>
        </section>
      ) : null}
    </>
  );
}
