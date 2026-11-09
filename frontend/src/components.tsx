import type { ReactNode } from "react";

export type Notify = (kind: "ok" | "error", title: string, message: string) => void;

export function Pill({ label, on }: { label: string; on: boolean }) {
  return (
    <span className={on ? "pill is-on" : "pill is-off"}>
      <span className="dot" />
      {label}
    </span>
  );
}

export function StatusBadge({ online }: { online: boolean }) {
  return (
    <span className={online ? "badge is-on" : "badge is-off"}>
      {online ? "Онлайн" : "Офлайн"}
    </span>
  );
}

export function Card({ children }: { children: ReactNode }) {
  return <div className="card">{children}</div>;
}

export function Section({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section className="section">
      <div className="section-head">
        <h2>{title}</h2>
        <span className="spacer" />
        {action}
      </div>
      {children}
    </section>
  );
}

export function Switch({
  checked,
  disabled,
  onToggle,
}: {
  checked: boolean;
  disabled?: boolean;
  onToggle: (next: boolean) => void;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      className={checked ? "switch is-on" : "switch"}
      onClick={() => onToggle(!checked)}
    />
  );
}
