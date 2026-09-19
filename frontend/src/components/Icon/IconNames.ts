import {
  BotIcon,
  BranchIcon,
  FilesIcon,
  GearIcon,
  GridIcon,
  SearchIcon,
  TerminalIcon,
} from "./IconSVG";

const iconNames = [
  {
    name: "file",
    component: FilesIcon,
  },
  {
    name: "search",
    component: SearchIcon,
  },
  {
    name: "branch",
    component: BranchIcon,
  },
  {
    name: "terminal",
    component: TerminalIcon,
  },
  {
    name: "gear",
    component: GearIcon,
  },
  {
    name: "bot",
    component: BotIcon,
  },
  {
    name: "grid",
    component: GridIcon,
  },
] as const;

export type IconName = (typeof iconNames)[number]["name"];

export { iconNames };
