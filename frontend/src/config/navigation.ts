import {
  LayoutDashboard,
  Server,
  Box,
  Cog,
  Monitor,
  Rocket,
  Zap,
  History,
  Settings,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

export interface NavItem {
  to: string;
  icon: LucideIcon;
  label: string;
}

export interface NavSection {
  heading?: string;
  items: NavItem[];
}

export const navSections: NavSection[] = [
  {
    items: [{ to: "/", icon: LayoutDashboard, label: "Dashboard" }],
  },
  {
    heading: "Infrastructure",
    items: [
      { to: "/targets", icon: Server, label: "Targets" },
      { to: "/templates", icon: Box, label: "Templates" },
      { to: "/factory", icon: Cog, label: "Template Factory" },
      { to: "/vms", icon: Monitor, label: "VMs" },
    ],
  },
  {
    heading: "Operations",
    items: [
      { to: "/deploy", icon: Rocket, label: "Deploy" },
      { to: "/actions", icon: Zap, label: "Actions" },
      { to: "/history", icon: History, label: "History" },
    ],
  },
  {
    heading: "System",
    items: [{ to: "/settings", icon: Settings, label: "Settings" }],
  },
];

// Flat list retained for mobile menu and command palette consumers
export const navItems: NavItem[] = navSections.flatMap((s) => s.items);
