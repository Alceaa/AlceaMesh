import { DeviceService, MetaService, TunnelService } from "../bindings/alceacore.ru/alceamesh";
import type {
  ConfigSummary,
  ConnectionStatus,
  DeviceView,
  NetworkView,
} from "../bindings/alceacore.ru/alceamesh";

export type { ConfigSummary, ConnectionStatus, DeviceView, NetworkView };

export function messageOf(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === "string") return error;
  return "unexpected error";
}

export const mesh = {
  version: (): Promise<string> => MetaService.Version(),
  coordinator: (): Promise<string> => MetaService.Coordinator(),

  status: (): Promise<ConnectionStatus> => TunnelService.Status(),
  connect: (): Promise<void> => TunnelService.Connect(),
  disconnect: (): Promise<void> => TunnelService.Disconnect(),
  importConfig: (): Promise<ConfigSummary> => TunnelService.Import(),
  forgetConfig: (): Promise<void> => TunnelService.Forget(),
  configText: (): Promise<string> => TunnelService.Config(),

  network: (): Promise<NetworkView> => DeviceService.Network(),
  setAccept: (accept: boolean): Promise<void> => DeviceService.SetAccept(accept),
  setName: (name: string): Promise<void> => DeviceService.SetName(name),
};
