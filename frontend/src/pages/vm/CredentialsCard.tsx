import { useState } from "react";
import { vms as vmApi } from "@/api/client";
import { useToast } from "@/components/ui/toast";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { KeyRound, Eye, EyeOff, Copy, Terminal } from "lucide-react";
import { getErrorMessage, copyText } from "@/lib/utils";

/** Initial-credentials reveal/copy panel for the VM overview sidebar. */
export function CredentialsCard({ vmId, vmIp }: { vmId: number; vmIp?: string }) {
  const { toast } = useToast();
  const [creds, setCreds] = useState<{ username: string; password: string } | null>(null);
  const [showPwd, setShowPwd] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const reveal = async () => {
    setLoading(true);
    setError("");
    try {
      const res = await vmApi.credentials(vmId);
      setCreds(res.data);
    } catch {
      setError("No credentials available for this VM");
    } finally {
      setLoading(false);
    }
  };

  const copyToClipboard = (text: string) =>
    copyText(text).then(() => toast("Copied to clipboard"), (e) => toast(getErrorMessage(e, "Failed to copy"), "error"));

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <KeyRound className="h-4 w-4" /> SSH Credentials
        </CardTitle>
      </CardHeader>
      <CardContent>
        {!creds && !error && (
          <Button size="sm" variant="outline" onClick={reveal} disabled={loading}>
            <Eye className="h-3 w-3 mr-1" /> {loading ? "Loading..." : "Reveal Credentials"}
          </Button>
        )}
        {error && <p className="text-sm text-muted-foreground">{error}</p>}
        {creds && (
          <div className="space-y-3">
            <div className="flex items-center gap-2">
              <Label className="w-20 text-xs text-muted-foreground">Username</Label>
              <code className="flex-1 bg-muted px-2 py-1 rounded text-sm">{creds.username}</code>
              <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => copyToClipboard(creds.username)} aria-label="Copy username">
                <Copy className="h-3 w-3" />
              </Button>
            </div>
            <div className="flex items-center gap-2">
              <Label className="w-20 text-xs text-muted-foreground">Password</Label>
              <code className="flex-1 bg-muted px-2 py-1 rounded text-sm font-mono">
                {showPwd
                  ? creds.password.split("").map((ch, i) => (
                      <span
                        key={i}
                        className={
                          /[0-9]/.test(ch)
                            ? "text-info"
                            : /[a-zA-Z]/.test(ch)
                              ? "text-success"
                              : "text-warning"
                        }
                      >
                        {ch}
                      </span>
                    ))
                  : "\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022"}
              </code>
              <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => setShowPwd(!showPwd)} aria-label={showPwd ? "Hide password" : "Show password"}>
                {showPwd ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
              </Button>
              <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => copyToClipboard(creds.password)} aria-label="Copy password">
                <Copy className="h-3 w-3" />
              </Button>
            </div>
            {vmIp && (
              <Button
                size="sm"
                variant="outline"
                className="w-full mt-1"
                onClick={() => copyToClipboard(`ssh ${creds.username}@${vmIp}`)}
              >
                <Terminal className="h-3 w-3 mr-1" /> Copy SSH Command
              </Button>
            )}
            <p className="text-xs text-muted-foreground mt-2">
              Password is stored AES-256 encrypted. Only decrypted on reveal.
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
