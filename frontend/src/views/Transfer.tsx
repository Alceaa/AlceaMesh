import type { NetworkView } from "../api";
import { Card, StatusBadge } from "../components";
import { SendIcon } from "../icons";

type Props = {
  network: NetworkView | null;
};

export default function TransferView({ network }: Props) {
  const others = (network?.devices ?? []).filter((device) => !device.self);

  return (
    <>
      <section className="section">
        <div className="section-head">
          <h2>Отправка файла</h2>
        </div>
        <Card>
          <div className="empty">
            Выберите устройство, готовое принять файл. Прямая передача появится на следующем этапе.
          </div>
        </Card>
      </section>

      <section className="section">
        <div className="section-head">
          <h2>Доступные устройства</h2>
        </div>
        <Card>
          {others.length > 0 ? (
            <div className="devices">
              {others.map((device) => (
                <div className="device" key={device.id}>
                  <span className="device-icon">
                    <SendIcon size={20} />
                  </span>
                  <div className="device-main">
                    <div className="device-name">{device.name}</div>
                    <div className="device-sub">{device.meshIP || "—"}</div>
                  </div>
                  <span className="status">
                    <StatusBadge online={device.online} />
                  </span>
                </div>
              ))}
            </div>
          ) : (
            <div className="empty">Сейчас нет устройств, готовых принять файл.</div>
          )}
        </Card>
      </section>
    </>
  );
}
