import { useState, type FormEvent } from "react";
import { Navigate } from "react-router-dom";
import { useAuth } from "@/hooks/useAuth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ShieldCheck, Server, Rocket, Zap } from "lucide-react";
import { ForgemillLogo } from "@/components/ForgemillLogo";

export default function Login() {
  const { user, login } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  if (user) return <Navigate to="/" replace />;

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    setLoading(true);
    try {
      await login(username, password);
    } catch {
      setError("Invalid credentials");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen grid lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] bg-background">
      {/* Brand panel — hidden below lg so the form is the whole story on small screens. */}
      <aside className="hidden lg:flex flex-col justify-between p-10 bg-sidebar border-r border-sidebar-border relative overflow-hidden">
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 opacity-[0.35]"
          style={{
            backgroundImage:
              "linear-gradient(hsl(var(--border)) 1px, transparent 1px), linear-gradient(90deg, hsl(var(--border)) 1px, transparent 1px)",
            backgroundSize: "32px 32px",
            maskImage: "radial-gradient(ellipse at 30% 20%, black 20%, transparent 70%)",
          }}
        />
        <div className="relative flex items-center gap-2.5">
          <ForgemillLogo size={28} />
          <span className="text-base font-semibold tracking-tight">Forgemill</span>
        </div>
        <div className="relative max-w-md">
          <p className="text-2xs font-semibold uppercase tracking-[0.12em] text-primary mb-3">Infrastructure, forged to order.</p>
          <h1 className="text-3xl font-semibold tracking-tight leading-tight text-balance">
            One console for vCenter, ESXi and Proxmox.
          </h1>
          <ul className="mt-6 space-y-3 text-13 text-muted-foreground">
            <li className="flex items-start gap-2.5"><Server className="h-4 w-4 mt-0.5 text-primary shrink-0" /> Build golden templates from ISO, keep them versioned and current.</li>
            <li className="flex items-start gap-2.5"><Rocket className="h-4 w-4 mt-0.5 text-primary shrink-0" /> Deploy with cloud-init and pre-flight checks that catch mistakes before anything is created.</li>
            <li className="flex items-start gap-2.5"><Zap className="h-4 w-4 mt-0.5 text-primary shrink-0" /> Run audited, parameterised actions across the fleet over SSH.</li>
          </ul>
        </div>
        <div className="relative text-2xs text-muted-foreground/70">Self-hosted · single container · RBAC + audit log</div>
      </aside>

      {/* Form */}
      <div className="flex items-center justify-center p-6">
        <div className="w-full max-w-[360px]">
          <div className="lg:hidden flex items-center gap-2.5 mb-8 justify-center">
            <ForgemillLogo size={30} />
            <span className="text-lg font-semibold tracking-tight">Forgemill</span>
          </div>
          <h2 className="text-xl font-semibold tracking-tight">Sign in</h2>
          <p className="text-13 text-muted-foreground mt-1">Use your Forgemill account or directory credentials.</p>

          <form onSubmit={handleSubmit} className="mt-6 space-y-4">
            {error && (
              <div role="alert" className="text-13 text-destructive border border-destructive/30 bg-destructive/6 rounded-md px-3 py-2">
                {error}
              </div>
            )}
            <div className="space-y-1.5">
              <Label htmlFor="username">Username</Label>
              <Input
                id="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="Username"
                autoFocus
                required
                autoComplete="username"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="Password"
                required
                autoComplete="current-password"
              />
            </div>
            <Button type="submit" className="w-full" disabled={loading}>
              {loading ? "Signing in..." : "Sign in"}
            </Button>
            <div className="flex items-center justify-center gap-1.5 pt-1 text-2xs text-muted-foreground">
              <ShieldCheck className="h-3 w-3 text-success" />
              <span>Sessions expire after 1 hour</span>
            </div>
          </form>
        </div>
      </div>
    </div>
  );
}
