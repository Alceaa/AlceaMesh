import { useEffect, useState } from "react";
import { mesh, messageOf } from "../api";
import type { ConnectionStatus, NetworkTransport, NetworkView } from "../api";
import { Card } from "../components";
import type { Notify } from "../components";

type Props = {
  status: ConnectionStatus | null;
  network: NetworkView | null;
  transport: NetworkTransport | null;
  onRefresh: (quiet?: boolean) => Promise<void> | void;
  onNotify: Notify;
};

export default function SettingsView({ status, network, transport, onRefresh, onNotify }: Props) {
  const [name, setName] = useState(network?.identity.name ?? "");
  const [limit, setLimit] = useState("0");
  const [saving, setSaving] = useState(false);

  const identityName = network?.identity.name ?? "";
  useEffect(() => {
    if (identityName) setName(identityName);
  }, [identityName]);

  const limitKBps = transport?.speedLimitKBps ?? 0;
  useEffect(() => {
    setLimit(limitKBps > 0 ? String(Number((limitKBps / 1024).toFixed(2))) : "0");
  }, [limitKBps]);

  const configured = status?.configured ?? false;

  const saveName = async () => {
    setSaving(true);
    try {
      await mesh.setName(name);
      await onRefresh(true);
      onNotify("ok", "Настройки", "Имя сохранено");
    } catch (error) {
      onNotify("error", "Настройки", messageOf(error));
    } finally {
      setSaving(false);
    }
  };

  const changeInbox = async () => {
    try {
      const dir = await mesh.chooseInbox();
      if (!dir) {
        return;
      }
      await onRefresh(true);
      onNotify("ok", "Папка приёма", dir);
    } catch (error) {
      onNotify("error", "Папка приёма", messageOf(error));
    }
  };

  const openInbox = async () => {
    try {
      await mesh.openInbox();
    } catch (error) {
      onNotify("error", "Папка приёма", messageOf(error));
    }
  };

  const saveLimit = async () => {
    setSaving(true);
    try {
      const value = Number(limit.replace(",", "."));
      const kbps = Number.isFinite(value) && value > 0 ? Math.round(value * 1024) : 0;
      await mesh.setSpeedLimit(kbps);
      await onRefresh(true);
      onNotify("ok", "Скорость", kbps > 0 ? `Ограничение: ${limit} МБ/с` : "Ограничение снято");
    } catch (error) {
      onNotify("error", "Скорость", messageOf(error));
    } finally {
      setSaving(false);
    }
  };

  const importConfig = async () => {
    try {
      const summary = await mesh.importConfig();
      if (!summary.name) {
        return;
      }
      await onRefresh(false);
      onNotify("ok", "Конфигурация", `Загружена конфигурация «${summary.name}»`);
    } catch (error) {
      onNotify("error", "Конфигурация", messageOf(error));
    }
  };

  const forgetConfig = async () => {
    try {
      await mesh.forgetConfig();
      await onRefresh(false);
      onNotify("ok", "Конфигурация", "Конфигурация удалена");
    } catch (error) {
      onNotify("error", "Конфигурация", messageOf(error));
    }
  };

  return (
    <>
      <section className="section">
        <div className="section-head">
          <h2>Устройство</h2>
        </div>
        <Card>
          <div className="card-body">
            <div className="row">
              <span className="label">Имя</span>
              <span className="value">{identityName || "—"}</span>
            </div>
            <div className="row">
              <span className="label">VPN-адрес</span>
              <span className="value mono">{network?.identity.meshIP || "—"}</span>
            </div>
            <div className="row">
              <span className="label">Платформа</span>
              <span className="value">{network?.identity.platform || "—"}</span>
            </div>
          </div>
        </Card>
        <div className="field">
          <label>Отображаемое имя</label>
          <input value={name} onChange={(event) => setName(event.target.value)} placeholder="Мой компьютер" />
        </div>
        <button className="btn" disabled={saving} onClick={() => void saveName()}>
          Сохранить имя
        </button>
      </section>

      <section className="section">
        <div className="section-head">
          <h2>Приём файлов</h2>
        </div>
        <Card>
          <div className="card-body">
            <div className="row">
              <span className="label">Папка</span>
              <span className="value mono">{transport?.inbox || "—"}</span>
            </div>
          </div>
          <div className="card-foot">
            <button className="btn" onClick={() => void changeInbox()}>
              Изменить папку…
            </button>
            <button className="btn" onClick={() => void openInbox()}>
              Открыть папку
            </button>
          </div>
        </Card>
        <div className="field">
          <label>Ограничение скорости, МБ/с (0 — без ограничения)</label>
          <input
            value={limit}
            inputMode="decimal"
            placeholder="0"
            onChange={(event) => setLimit(event.target.value)}
          />
        </div>
        <button className="btn" disabled={saving} onClick={() => void saveLimit()}>
          Сохранить ограничение
        </button>
      </section>

      <section className="section">
        <div className="section-head">
          <h2>Конфигурация WireGuard</h2>
        </div>
        {configured ? (
          <>
            <Card>
              <div className="card-body">
                <div className="row">
                  <span className="label">Туннель</span>
                  <span className="value">{status?.name || "—"}</span>
                </div>
                <div className="row">
                  <span className="label">Сервер</span>
                  <span className="value mono">{status?.endpoint || "—"}</span>
                </div>
              </div>
            </Card>
            <button className="btn" onClick={() => void importConfig()}>
              Заменить конфигурацию…
            </button>
            <button className="btn btn-danger" onClick={() => void forgetConfig()}>
              Удалить конфигурацию
            </button>
          </>
        ) : (
          <button className="btn btn-primary" onClick={() => void importConfig()}>
            Импортировать .conf…
          </button>
        )}
      </section>
    </>
  );
}
