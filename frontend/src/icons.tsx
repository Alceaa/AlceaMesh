import type { ReactNode } from "react";

type IconProps = { size?: number };

function Svg({ size = 20, children }: IconProps & { children: ReactNode }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

export const LogoIcon = ({ size = 26 }: IconProps) => (
  <Svg size={size}>
    <circle cx="6" cy="6.4" r="2.4" />
    <circle cx="18" cy="6.4" r="2.4" />
    <circle cx="12" cy="17.8" r="2.4" />
    <path d="M8.2 7.4 10.3 15.6" />
    <path d="M15.8 7.4 13.7 15.6" />
    <path d="M8.4 6.4h7.2" />
  </Svg>
);

export const MonitorIcon = ({ size = 20 }: IconProps) => (
  <Svg size={size}>
    <rect x="3" y="4.5" width="18" height="12.5" rx="2" />
    <path d="M9 20.5h6" />
    <path d="M12 17v3.5" />
  </Svg>
);

export const SendIcon = ({ size = 21 }: IconProps) => (
  <Svg size={size}>
    <path d="M3.6 11.6 20.6 4.2l-7.1 17-2.6-7.5z" />
    <path d="M10.9 13.7 20.6 4.2" />
  </Svg>
);

export const GearIcon = ({ size = 21 }: IconProps) => (
  <Svg size={size}>
    <circle cx="12" cy="12" r="3.1" />
    <path d="M12 2.9v2.3M12 18.8v2.3M4.2 4.2l1.6 1.6M18.2 18.2l1.6 1.6M2.9 12h2.3M18.8 12h2.3M4.2 19.8l1.6-1.6M18.2 5.8l1.6-1.6" />
  </Svg>
);

export const FileIcon = ({ size = 20 }: IconProps) => (
  <Svg size={size}>
    <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
    <path d="M14 3v5h5" />
  </Svg>
);
