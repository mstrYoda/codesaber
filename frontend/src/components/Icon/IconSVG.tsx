import { iconProps } from "./constants";

type Icon = (props: React.SVGProps<SVGSVGElement>) => React.ReactElement



const FilesIcon: Icon = (props) => (
  <svg {...iconProps} {...props}>
    <rect x="8" y="3" width="13" height="13" rx="2" />
    <rect x="3" y="8" width="13" height="13" rx="2" />
  </svg>
)

const SearchIcon: Icon = (props) => (
  <svg {...iconProps} {...props}>
    <circle cx="11" cy="11" r="6.5" />
    <path d="M20 20l-3.8-3.8" />
  </svg>
)

const BranchIcon: Icon = (props) => (
  <svg {...iconProps} {...props}>
    <circle cx="6" cy="6" r="2.4" />
    <circle cx="6" cy="18" r="2.4" />
    <circle cx="18" cy="8" r="2.4" />
    <path d="M6 8.4v7.2" />
    <path d="M18 10.4c0 3.4-2.8 4.4-6.2 4.6" />
  </svg>
)

const TerminalIcon: Icon = (props) => (
  <svg {...iconProps} {...props}>
    <path d="M4 17l6-5-6-5" />
    <path d="M12 19h8" />
  </svg>
)

const GearIcon: Icon = (props) => (
  <svg {...iconProps} {...props}>
    <circle cx="12" cy="12" r="3" />
    <path d="M12 2.8v2.4M12 18.8v2.4M2.8 12h2.4M18.8 12h2.4M5.5 5.5l1.7 1.7M16.8 16.8l1.7 1.7M18.5 5.5l-1.7 1.7M7.2 16.8l-1.7 1.7" />
  </svg>
)

const BotIcon: Icon = (props) => (
  <svg {...iconProps} {...props}>
    <rect x="4" y="8" width="16" height="11" rx="3" />
    <path d="M12 8V4.5" />
    <circle cx="12" cy="3.5" r="1.2" />
    <circle cx="9" cy="13.5" r="1" fill="currentColor" stroke="none" />
    <circle cx="15" cy="13.5" r="1" fill="currentColor" stroke="none" />
  </svg>
)

const GridIcon: Icon = (props) => (
  <svg {...iconProps} {...props} fill="currentColor" stroke="none">
    {[6, 12, 18].flatMap((y) =>
      [6, 12, 18].map((x) => <circle key={`${x}-${y}`} cx={x} cy={y} r="1.6" />),
    )}
  </svg>
)

export { FilesIcon, SearchIcon, BranchIcon, TerminalIcon, GearIcon, BotIcon, GridIcon }