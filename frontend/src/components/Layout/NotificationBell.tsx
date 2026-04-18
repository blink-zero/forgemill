import { useEffect, useRef, useState, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { Bell, Check, CheckCheck, X, CircleAlert, CircleCheck, Info as InfoIcon, AlertTriangle } from "lucide-react";
import { notifications as notifApi } from "@/api/client";
import type { Notification, NotificationLevel } from "@/types";
import { cn } from "@/lib/utils";

const POLL_INTERVAL_MS = 30_000;

function timeAgo(iso: string): string {
  const then = new Date(iso).getTime();
  const diffS = Math.max(1, Math.floor((Date.now() - then) / 1000));
  if (diffS < 60) return `${diffS}s ago`;
  const diffM = Math.floor(diffS / 60);
  if (diffM < 60) return `${diffM}m ago`;
  const diffH = Math.floor(diffM / 60);
  if (diffH < 24) return `${diffH}h ago`;
  const diffD = Math.floor(diffH / 24);
  if (diffD < 7) return `${diffD}d ago`;
  return new Date(iso).toLocaleDateString();
}

function iconForLevel(level: NotificationLevel) {
  switch (level) {
    case "success": return <CircleCheck className="h-4 w-4 text-success shrink-0" />;
    case "warning": return <AlertTriangle className="h-4 w-4 text-warning shrink-0" />;
    case "error":   return <CircleAlert className="h-4 w-4 text-destructive shrink-0" />;
    default:        return <InfoIcon className="h-4 w-4 text-info shrink-0" />;
  }
}

export function NotificationBell() {
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<Notification[]>([]);
  const [unread, setUnread] = useState(0);
  const [loading, setLoading] = useState(false);
  const panelRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);

  const loadFull = useCallback(async () => {
    setLoading(true);
    try {
      const res = await notifApi.list({ limit: 30 });
      setItems(res.data.notifications);
      setUnread(res.data.unread_count);
    } catch {
      // ignore — widget is non-critical
    } finally {
      setLoading(false);
    }
  }, []);

  const loadCountOnly = useCallback(async () => {
    try {
      const res = await notifApi.unreadCount();
      setUnread(res.data.unread_count);
    } catch {
      // ignore
    }
  }, []);

  useEffect(() => {
    loadCountOnly();
    const t = setInterval(() => {
      // If the panel is open, refresh the full list so new items show up
      if (open) {
        loadFull();
      } else {
        loadCountOnly();
      }
    }, POLL_INTERVAL_MS);
    return () => clearInterval(t);
  }, [open, loadCountOnly, loadFull]);

  useEffect(() => {
    if (open) loadFull();
  }, [open, loadFull]);

  // Close on Escape or outside click
  useEffect(() => {
    if (!open) return;
    const onEsc = (e: KeyboardEvent) => { if (e.key === "Escape") setOpen(false); };
    const onClick = (e: MouseEvent) => {
      const t = e.target as Node;
      if (
        panelRef.current && !panelRef.current.contains(t) &&
        (!buttonRef.current || !buttonRef.current.contains(t))
      ) {
        setOpen(false);
      }
    };
    document.addEventListener("keydown", onEsc);
    document.addEventListener("mousedown", onClick);
    return () => {
      document.removeEventListener("keydown", onEsc);
      document.removeEventListener("mousedown", onClick);
    };
  }, [open]);

  const markRead = async (id: number) => {
    setItems((prev) => prev.map((n) => (n.id === id ? { ...n, is_read: true } : n)));
    setUnread((u) => Math.max(0, u - 1));
    try { await notifApi.markRead(id); } catch { /* best effort */ }
  };

  const markAllRead = async () => {
    setItems((prev) => prev.map((n) => ({ ...n, is_read: true })));
    setUnread(0);
    try { await notifApi.markAllRead(); } catch { /* best effort */ }
  };

  const remove = async (id: number) => {
    const target = items.find((n) => n.id === id);
    setItems((prev) => prev.filter((n) => n.id !== id));
    if (target && !target.is_read) setUnread((u) => Math.max(0, u - 1));
    try { await notifApi.delete(id); } catch { /* best effort */ }
  };

  const openItem = (n: Notification) => {
    if (!n.is_read) markRead(n.id);
    if (n.link) {
      setOpen(false);
      navigate(n.link);
    }
  };

  return (
    <div className="relative">
      <button
        ref={buttonRef}
        onClick={() => setOpen((v) => !v)}
        className="relative inline-flex h-9 w-9 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        aria-label={`Notifications${unread > 0 ? ` (${unread} unread)` : ""}`}
        aria-expanded={open}
      >
        <Bell className="h-4 w-4" />
        {unread > 0 && (
          <span className="absolute -top-0.5 -right-0.5 inline-flex min-w-[17px] h-[17px] items-center justify-center rounded-full bg-destructive px-1 text-[10px] font-semibold text-destructive-foreground leading-none">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>

      {open && (
        <div
          ref={panelRef}
          role="dialog"
          aria-label="Notifications"
          className="absolute right-0 top-full mt-2 w-[380px] max-w-[calc(100vw-1rem)] rounded-md border border-border bg-popover text-popover-foreground shadow-lg z-50"
        >
          <div className="flex items-center justify-between px-4 py-2.5 border-b border-border">
            <div className="text-sm font-semibold">Notifications</div>
            <button
              onClick={markAllRead}
              disabled={unread === 0}
              className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground disabled:opacity-40 disabled:cursor-default transition-colors"
              title="Mark all as read"
            >
              <CheckCheck className="h-3.5 w-3.5" />
              Mark all read
            </button>
          </div>

          <div className="max-h-[60vh] overflow-y-auto">
            {loading && items.length === 0 ? (
              <div className="px-4 py-8 text-center text-sm text-muted-foreground">Loading...</div>
            ) : items.length === 0 ? (
              <div className="px-4 py-10 text-center">
                <Bell className="h-6 w-6 text-muted-foreground mx-auto mb-2 opacity-60" />
                <div className="text-sm font-medium">You're all caught up</div>
                <div className="text-xs text-muted-foreground mt-1">No notifications right now.</div>
              </div>
            ) : (
              <ul className="divide-y divide-border">
                {items.map((n) => (
                  <li
                    key={n.id}
                    className={cn(
                      "group relative px-4 py-3 hover:bg-accent/50 transition-colors",
                      !n.is_read && "bg-primary/5"
                    )}
                  >
                    <button
                      onClick={() => openItem(n)}
                      className="flex items-start gap-3 w-full text-left"
                    >
                      {iconForLevel(n.level)}
                      <div className="flex-1 min-w-0">
                        <div className="flex items-start justify-between gap-2">
                          <p className="text-sm font-medium truncate">{n.title}</p>
                          {!n.is_read && (
                            <span className="mt-1.5 h-1.5 w-1.5 rounded-full bg-primary shrink-0" aria-label="Unread" />
                          )}
                        </div>
                        {n.body && (
                          <p className="text-xs text-muted-foreground mt-0.5 line-clamp-2">{n.body}</p>
                        )}
                        <p className="text-[11px] text-muted-foreground/80 mt-1">{timeAgo(n.created_at)}</p>
                      </div>
                    </button>
                    <div className="absolute top-2 right-2 flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
                      {!n.is_read && (
                        <button
                          onClick={(e) => { e.stopPropagation(); markRead(n.id); }}
                          className="p-1 rounded hover:bg-accent text-muted-foreground hover:text-foreground"
                          title="Mark read"
                        >
                          <Check className="h-3 w-3" />
                        </button>
                      )}
                      <button
                        onClick={(e) => { e.stopPropagation(); remove(n.id); }}
                        className="p-1 rounded hover:bg-accent text-muted-foreground hover:text-foreground"
                        title="Dismiss"
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
