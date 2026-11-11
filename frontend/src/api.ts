import { DeviceService, MetaService, PeerService, TunnelService } from "../bindings/alceacore.ru/alceamesh";
import type {
  ConfigSummary,
  ConnectionStatus,
  DeviceView,
  NetworkTransport,
  NetworkView,
  PeerStatus,
  ServiceState,
  TransferProgress,
} from "../bindings/alceacore.ru/alceamesh";

export type {
  ConfigSummary,
  ConnectionStatus,
  DeviceView,
  NetworkTransport,
  NetworkView,
  PeerStatus,
  ServiceState,
  TransferProgress,
};

export function messageOf(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === "string") return error;
  return "Неизвестная ошибка";
}

export const mesh = {
  version: (): Promise<string> => MetaService.Version(),
  coordinator: (): Promise<string> => MetaService.Coordinator(),
  autostart: (): Promise<boolean> => MetaService.Autostart(),
  setAutostart: (enabled: boolean): Promise<void> => MetaService.SetAutostart(enabled),
  tray: (): Promise<boolean> => MetaService.Tray(),
  hideToTray: (): Promise<void> => MetaService.HideToTray(),
  startService: (asAdmin: boolean): Promise<ServiceState> => MetaService.StartService(asAdmin),
  serviceRunning: (): Promise<boolean> => MetaService.ServiceRunning(),

  status: (): Promise<ConnectionStatus> => TunnelService.Status(),
  connect: (): Promise<void> => TunnelService.Connect(),
  disconnect: (): Promise<void> => TunnelService.Disconnect(),
  importConfig: (): Promise<ConfigSummary> => TunnelService.Import(),
  forgetConfig: (): Promise<void> => TunnelService.Forget(),
  configText: (): Promise<string> => TunnelService.Config(),

  network: (): Promise<NetworkView> => DeviceService.Network(),
  setAccept: (accept: boolean): Promise<void> => DeviceService.SetAccept(accept),
  setName: (name: string): Promise<void> => DeviceService.SetName(name),

  transport: (): Promise<NetworkTransport> => PeerService.Transport(),
  connectPeer: (peerKey: string, name: string, meshIP: string): Promise<void> =>
    PeerService.Connect(peerKey, name, meshIP),
  disconnectPeer: (peerKey: string): Promise<void> => PeerService.Disconnect(peerKey),
  sendFiles: (peerKey: string): Promise<TransferProgress> => PeerService.SendFiles(peerKey),
  sendFolder: (peerKey: string): Promise<TransferProgress> => PeerService.SendFolder(peerKey),
  clearFinished: (): Promise<void> => PeerService.ClearFinished(),

  inbox: (): Promise<string> => PeerService.Inbox(),
  chooseInbox: (): Promise<string> => PeerService.ChooseInbox(),
  openInbox: (): Promise<void> => PeerService.OpenInbox(),
  setSpeedLimit: (kilobytesPerSecond: number): Promise<void> =>
    PeerService.SetSpeedLimit(kilobytesPerSecond),
};
