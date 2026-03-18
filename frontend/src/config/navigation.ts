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

export const navItems: NavItem[] = [
  { to: "/", icon: LayoutDashboard, label: "Dashboard" },
  { to: "/targets", icon: Server, label: "Targets" },
  { to: "/templates", icon: Box, label: "Templates" },
  { to: "/factory", icon: Cog, label: "Template Factory" },
  { to: "/vms", icon: Monitor, label: "VMs" },
  { to: "/deploy", icon: Rocket, label: "Deploy" },
  { to: "/actions", icon: Zap, label: "Actions" },
  { to: "/history", icon: History, label: "History" },
  { to: "/settings", icon: Settings, label: "Settings" },
];
