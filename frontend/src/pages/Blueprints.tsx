import { useEffect, useState } from "react";
import { blueprints, templates as templatesApi, targets as targetsApi } from "@/api/client";
import type { Blueprint, Template, Target } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Plus, Rocket, Trash2, Edit2, Info, Loader2 } from "lucide-react";
import { ViewToggle } from "@/components/ui/view-toggle";
import { Select } from "@/components/ui/select";

export default function BlueprintsPage() {
  const [bpList, setBpList] = useState<Blueprint[]>([]);
  const [templatesList, setTemplatesList] = useState<Template[]>([]);
  const [targetsList, setTargetsList] = useState<Target[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [deployingId, setDeployingId] = useState<number | null>(null);
  const [deployName, setDeployName] = useState("");
  const [form, setForm] = useState({ name: "", description: "", template_id: "", target_id: "", config_json: "{}" });

  const fetchData = async () => {
    try {
      const [bpRes, tplRes, tgtRes] = await Promise.all([
        blueprints.list(),
        templatesApi.list(),
        targetsApi.list(),
      ]);
      setBpList(bpRes.data || []);
      setTemplatesList(tplRes.data || []);
      setTargetsList(tgtRes.data || []);
    } catch {
      // Ensure UI doesn't get stuck on loading spinner
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, []);

  const handleCreate = async () => {
    await blueprints.create({
      name: form.name,
      description: form.description,
      template_id: form.template_id ? parseInt(form.template_id) : null,
      target_id: form.target_id ? parseInt(form.target_id) : null,
      config_json: form.config_json || "{}",
    });
    setShowForm(false);
    setForm({ name: "", description: "", template_id: "", target_id: "", config_json: "{}" });
    fetchData();
  };

  const handleDelete = async (id: number) => {
    if (!confirm("Delete this blueprint?")) return;
    await blueprints.delete(id);
    fetchData();
  };

  const handleDeploy = async (id: number) => {
    if (!deployName) return;
    await blueprints.deploy(id, { vm_name: deployName });
    setDeployingId(null);
    setDeployName("");
  };

  if (loading) {
    return <div className="flex items-center justify-center h-64"><Loader2 className="h-8 w-8 animate-spin text-primary" /></div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Blueprints</h1>
        <div className="flex items-center gap-2">
          <ViewToggle />
          <Button onClick={() => setShowForm(!showForm)}><Plus className="h-4 w-4 mr-1" /> New Blueprint</Button>
        </div>
      </div>

      {/* Info card explaining blueprints */}
      <Card className="border-blue-200 dark:border-blue-900 bg-blue-50/50 dark:bg-blue-950/20">
        <CardContent className="flex gap-4 p-4">
          <Info className="h-5 w-5 text-blue-500 mt-0.5 shrink-0" />
          <div className="space-y-2 text-sm">
            <p className="font-medium text-blue-900 dark:text-blue-100">What are Blueprints?</p>
            <p className="text-blue-800 dark:text-blue-200">
              Blueprints are reusable deployment configurations that save your preferred template, target, and VM settings.
              Instead of filling out the deployment form each time, create a blueprint once and use <strong>Quick Deploy</strong> to
              spin up VMs with a single click.
            </p>
            <ul className="text-blue-700 dark:text-blue-300 space-y-1 list-disc list-inside">
              <li>Select a <strong>template</strong> (the OS image) and a <strong>target</strong> (where to deploy)</li>
              <li>Add custom configuration as JSON (CPU, memory, network, etc.)</li>
              <li>Use Quick Deploy to launch a VM &mdash; just provide a name</li>
            </ul>
          </div>
        </CardContent>
      </Card>

      {showForm && (
        <Card>
          <CardHeader><CardTitle>Create Blueprint</CardTitle></CardHeader>
          <CardContent className="space-y-4">
            <div>
              <Label>Name</Label>
              <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </div>
            <div>
              <Label>Description</Label>
              <Input value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <div>
                <Label>Template</Label>
                <Select value={form.template_id} onChange={(e) => setForm({ ...form, template_id: e.target.value })}>
                  <option value="">Select template...</option>
                  {templatesList.map((t) => <option key={t.id} value={t.id}>{t.name} ({t.target_name})</option>)}
                </Select>
              </div>
              <div>
                <Label>Target</Label>
                <Select value={form.target_id} onChange={(e) => setForm({ ...form, target_id: e.target.value })}>
                  <option value="">Select target...</option>
                  {targetsList.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
                </Select>
              </div>
            </div>
            <div>
              <Label>Config JSON</Label>
              <textarea className="w-full rounded-md border px-3 py-2 text-sm font-mono h-32" value={form.config_json} onChange={(e) => setForm({ ...form, config_json: e.target.value })} />
            </div>
            <Button onClick={handleCreate} disabled={!form.name}>Create</Button>
          </CardContent>
        </Card>
      )}

      {bpList.length === 0 ? (
        <Card>
          <CardContent className="py-10 text-center text-muted-foreground">
            No blueprints yet. Create one to save reusable deployment configurations.
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {bpList.map((bp) => (
            <Card key={bp.id}>
              <CardHeader className="pb-2">
                <div className="flex items-center justify-between">
                  <CardTitle className="text-base">{bp.name}</CardTitle>
                  <Button variant="ghost" size="sm" onClick={() => handleDelete(bp.id)}><Trash2 className="h-4 w-4 text-destructive" /></Button>
                </div>
              </CardHeader>
              <CardContent className="space-y-3">
                <p className="text-xs text-muted-foreground">{bp.description || "No description"}</p>
                <div className="text-xs space-y-1">
                  {bp.template_name && <p>Template: {bp.template_name}</p>}
                  {bp.target_name && <p>Target: {bp.target_name}</p>}
                </div>
                {deployingId === bp.id ? (
                  <div className="flex gap-2">
                    <Input placeholder="VM name" value={deployName} onChange={(e) => setDeployName(e.target.value)} className="text-sm" />
                    <Button size="sm" onClick={() => handleDeploy(bp.id)} disabled={!deployName}><Rocket className="h-4 w-4" /></Button>
                  </div>
                ) : (
                  <Button size="sm" className="w-full" onClick={() => setDeployingId(bp.id)}>
                    <Rocket className="h-4 w-4 mr-1" /> Quick Deploy
                  </Button>
                )}
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
