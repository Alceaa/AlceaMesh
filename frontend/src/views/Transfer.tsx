import { useState } from "react";
import { mesh, messageOf } from "../api";
import type { DeviceView, NetworkTransport, NetworkView } from "../api";
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

export default function TransferView({ network, transport, onRefresh, onNotify }: Props) {
  const [pending, setPending] = useState("");

  const others = (network?.devices ?? []).filter((device) => !device.self);

  const statusOf = (peerKey: string) => (transport?.peers ?? []).find((item) => item.peer === peerKey);

  const connect = async (device: DeviceView) => {
    setPending(device.publicKey);
    try {
      await mesh.connectPeer(device.publicKey, device.name, device.meshIP);
      onNotify("ok", "Прямое соединение", `Пробиваем NAT до «${device.name}»`);
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

  return (
    <>
      <section className="section">
        <div className="section-head">
          <h2>Отправка файла</h2>
        </div>
        <Card>
          <div className="empty">
            Выберите устройство и установите прямое соединение. Сама передача файлов появится на
            следующем этапе.
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
                    <span className="status">
                      {state === "idle" ? (
                        <button
                          className="btn chip"
                          disabled={pending === device.publicKey || !device.publicKey}
                          onClick={() => void connect(device)}
                        >
                          Соединить
                        </button>
                      ) : (
                        <button
                          className={state === "direct" ? "badge is-on" : "badge"}
                          onClick={() => void disconnect(device)}
                        >
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
    </>
  );
}
