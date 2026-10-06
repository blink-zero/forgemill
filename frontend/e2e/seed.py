#!/usr/bin/env python3
"""Seed a freshly-migrated Forgemill database with a fixed lab dataset.

Usage: seed.py <path/to/forgemill.db>

Run the server once against an empty data dir first (it creates the schema and
the admin user), stop it, run this, then start it again — run.sh does exactly
that. Everything inserted is fictional (lab.internal hostnames, RFC1918
addresses, placeholder credential blobs). Timestamps are relative to a single
reference time that is written next to the DB as `seed-time` so the browser
clock can be pinned to it and relative labels ("12d ago") render identically on
every run.
"""
import json
import os
import sqlite3
import sys
from datetime import datetime, timedelta, timezone

if len(sys.argv) != 2:
    sys.exit(__doc__)
DB = sys.argv[1]
for ext in ("-wal", "-shm"):
    if os.path.exists(DB + ext):
        os.remove(DB + ext)

c = sqlite3.connect(DB)
c.execute("PRAGMA foreign_keys=ON")
now = datetime.now(timezone.utc).replace(microsecond=0, tzinfo=None)
with open(os.path.join(os.path.dirname(DB), "seed-time"), "w") as f:
    f.write(now.strftime("%Y-%m-%dT%H:%M:%SZ"))


def ts(**kw):
    return (now - timedelta(**kw)).strftime("%Y-%m-%d %H:%M:%S")


# Not a real secret: the UI never decrypts target credentials on the pages the
# tour visits (resource lookups are mocked in the browser).
PW_BLOB = "e2e-placeholder-not-a-real-secret"

for t in ["action_executions", "vm_snapshots", "managed_vms", "deployment_logs", "deployments",
          "blueprints", "notifications", "templates", "template_builds", "targets"]:
    c.execute(f"DELETE FROM {t}")

c.execute("""INSERT INTO targets (id,name,type,hostname,port,username,password_encrypted,validate_certs,is_default,status,last_connected_at,created_at,updated_at,datacenter,datastore,network)
             VALUES (1,'vcenter-lab','vcenter','vcsa.lab.internal',443,'administrator@vsphere.local',?,0,1,'connected',?,?,?,'Lab-DC','ds-nvme-01','VM Network')""",
          (PW_BLOB, ts(minutes=3), ts(days=120), ts(minutes=3)))
c.execute("""INSERT INTO targets (id,name,type,hostname,port,username,password_encrypted,validate_certs,is_default,status,last_connected_at,created_at,updated_at,storage_pool,network_bridge)
             VALUES (2,'pve-01','proxmox','pve-01.lab.internal',8006,'forgemill@pve',?,0,0,'connected',?,?,?,'local-zfs','vmbr0')""",
          (PW_BLOB, ts(minutes=3), ts(days=95), ts(minutes=3)))
# Unmanaged VMs seen on the last sync (drives the Targets badge, VMs → Discover picker and the Dashboard tile).
c.execute("UPDATE targets SET unmanaged_vms = 3, unmanaged_checked_at = ? WHERE id = 1", (ts(minutes=3),))
# pve-01 is fine; vcenter-lab runs on an evaluation with nine days left (badge + Dashboard tile).
c.execute("UPDATE targets SET license_edition = 'Evaluation Mode', evaluation_expires_at = ? WHERE id = 1", ((now + timedelta(days=9)).strftime("%Y-%m-%d %H:%M:%S"),))
# A free-licensed standalone ESXi host: reachable, inventory-only.
c.execute("""INSERT INTO targets (id,name,type,hostname,port,username,password_encrypted,validate_certs,is_default,status,last_connected_at,created_at,updated_at,datacenter,datastore,network,license_edition,deploy_supported,capability_note)
             VALUES (3,'esxi-free-lab','esxi','esxi-free.lab.internal',443,'root',?,0,0,'connected',?,?,?,'ha-datacenter','datastore1','VM Network','VMware vSphere 8 Hypervisor',0,
             'This ESXi host is on the free vSphere Hypervisor license, which prohibits vSphere API write operations. Forgemill can read its inventory, sync, discover and adopt VMs, but cannot deploy, power, reconfigure, snapshot or destroy VMs on it. Assign a paid or VMUG license key to the host, or manage it through vCenter.')""",
          (PW_BLOB, ts(minutes=3), ts(days=30), ts(minutes=3)))

c.execute("""INSERT INTO template_builds (id,os_definition_id,target_id,status,template_name,config_json,iso_url,iso_checksum,started_at,completed_at,created_by,created_at,version,auto_triggered)
             VALUES (1,'ubuntu-24.04',1,'completed','ubuntu-24.04-cloudinit','{}','https://releases.ubuntu.com/24.04/ubuntu-24.04.3-live-server-amd64.iso','sha256:c3514bf0056180d09376462a7a1b4f213c1d6e8ea67fae5c25099c6fd3d8274b',?,?,1,?,3,0)""",
          (ts(days=6, hours=2), ts(days=6, hours=1, minutes=12), ts(days=6, hours=2)))

templates = [
    (1, 1, "ubuntu-24.04-cloudinit", "vm-2041", "ubuntu64Guest", "Ubuntu 24.04 LTS", 2, 4096, 40, 1, 1, 3, ts(days=6, hours=1)),
    (2, 1, "ubuntu-22.04-cloudinit", "vm-1873", "ubuntu64Guest", "Ubuntu 22.04 LTS", 2, 4096, 40, None, 1, 2, ts(days=41)),
    (3, 1, "rocky-9-cloudinit", "vm-2102", "rockylinux64Guest", "Rocky Linux 9", 2, 4096, 40, None, 1, 1, ts(days=20)),
    (4, 2, "debian-12-cloudinit", "9001", "l26", "Debian 12", 2, 2048, 32, None, 0, 1, None),
    (5, 2, "ubuntu-24.04-cloudinit", "9002", "l26", "Ubuntu 24.04 LTS", 2, 4096, 40, None, 0, 1, None),
]
for (i, tid, name, moref, guest, osname, cpu, mem, disk, build, managed, ver, built) in templates:
    c.execute("""INSERT INTO templates (id,target_id,name,moref,os_type,os_name,guest_id,cpu,memory_mb,disk_gb,notes,icon,last_synced_at,created_at,build_id,managed_by_forgemill,version,built_at,lifecycle_status,platform)
                 VALUES (?,?,?,?,?,?,?,?,?,?,'', 'linux',?,?,?,?,?,?,'active','linux')""",
              (i, tid, name, moref, "linux", osname, guest, cpu, mem, disk, ts(minutes=3), ts(days=90), build, managed, ver, built))
c.execute("UPDATE template_builds SET template_id=1 WHERE id=1")

vms = [
    # id, name, target, template, ref, state, ip, cpu, mem, disk, os, created_days_ago, on_days_ago(None=off), runtime_h, status
    (1, "web-01", 1, 1, "vm-3101", "poweredOn", "10.20.10.11", 2, 4096, 40, "Ubuntu 24.04 LTS", 38, 12, 812, "completed"),
    (2, "web-02", 1, 1, "vm-3102", "poweredOn", "10.20.10.12", 2, 4096, 40, "Ubuntu 24.04 LTS", 38, 12, 809, "completed"),
    (3, "api-01", 1, 1, "vm-3110", "poweredOn", "10.20.10.21", 4, 8192, 60, "Ubuntu 24.04 LTS", 30, 30, 718, "completed"),
    (4, "db-01", 1, 3, "vm-3120", "poweredOn", "10.20.10.31", 8, 32768, 200, "Rocky Linux 9", 29, 29, 695, "completed"),
    (5, "cache-01", 1, 2, "vm-3130", "suspended", "10.20.10.41", 2, 8192, 40, "Ubuntu 22.04 LTS", 25, None, 410, "completed"),
    (6, "ci-runner-01", 2, 5, "9103", "poweredOn", "10.20.20.15", 4, 8192, 80, "Ubuntu 24.04 LTS", 14, 2, 301, "completed"),
    (7, "monitoring-01", 2, 4, "9104", "poweredOn", "10.20.20.16", 2, 4096, 50, "Debian 12", 14, 14, 335, "completed"),
    (8, "staging-app-03", 1, 1, "vm-3140", "poweredOff", "", 2, 4096, 40, "Ubuntu 24.04 LTS", 9, None, 96, "completed"),
    (9, "staging-app-04", 1, 1, "", "poweredOff", "", 2, 4096, 40, "Ubuntu 24.04 LTS", 1, None, 0, "failed"),
]
for (i, name, tid, tpl, ref, state, ip, cpu, mem, disk, osn, cdays, ondays, rt_h, dstatus) in vms:
    tname = [t[2] for t in templates if t[0] == tpl][0]
    cfg = {"vm_name": name, "cpu": cpu, "memory_mb": mem, "disk_gb": disk, "network": "VM Network" if tid == 1 else "vmbr0",
           "datastore": "ds-nvme-01" if tid == 1 else "local-zfs", "ip_address": ip, "hostname": name, "domain_name": "lab.internal"}
    started = ts(days=cdays, minutes=4)
    done = ts(days=cdays)
    c.execute("""INSERT INTO deployments (id,template_id,target_id,vm_name,status,config_json,started_at,completed_at,error_message,created_by,created_at,initial_username,template_name)
                 VALUES (?,?,?,?,?,?,?,?,?,1,?,'forgemill',?)""",
              (i, tpl, tid, name, dstatus, json.dumps(cfg), started, done,
               "clone failed: datastore 'ds-nvme-01' has insufficient free space (needs 40 GB, 31 GB free)" if dstatus == "failed" else None,
               started, tname))
    for j, (lvl, msg) in enumerate([("info", f"Starting deployment of {name} from {tname}"), ("info", "Cloning template…"),
                                    ("info", "Applying cloud-init configuration"), ("info", "Powering on"),
                                    ("info", f"Guest reported IP {ip}" if ip else "Waiting for guest IP"),
                                    ("info", "Deployment completed") if dstatus == "completed" else ("error", "Deployment failed")]):
        c.execute("INSERT INTO deployment_logs (deployment_id,timestamp,level,message) VALUES (?,?,?,?)",
                  (i, (now - timedelta(days=cdays, minutes=4) + timedelta(seconds=35 * j)).strftime("%Y-%m-%d %H:%M:%S"), lvl, msg))
    if dstatus != "completed":
        continue
    on_at = ts(days=ondays) if ondays is not None else None
    off_at = None if state == "poweredOn" else ts(days=3 if state == "poweredOff" else 0, hours=0 if state == "poweredOff" else 18)
    changed = on_at if state == "poweredOn" else off_at
    c.execute("""INSERT INTO managed_vms (id,deployment_id,target_id,vm_name,vm_ref,power_state,ip_address,cpu,memory_mb,disk_gb,os_type,last_synced_at,created_at,platform,state_changed_at,last_powered_on_at,last_powered_off_at,total_runtime_seconds)
                 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,'linux',?,?,?,?)""",
              (i, i, tid, name, ref, state, ip, cpu, mem, disk, osn, ts(minutes=2), done, changed,
               on_at or ts(days=cdays), off_at, rt_h * 3600))

# An adopted VM on the inventory-only ESXi host (no deployment): the VM page
# shows the banner and disables hypervisor controls.
c.execute("""INSERT INTO managed_vms (id,deployment_id,target_id,vm_name,vm_ref,power_state,ip_address,cpu,memory_mb,disk_gb,os_type,last_synced_at,created_at,platform,state_changed_at,last_powered_on_at,last_powered_off_at,total_runtime_seconds,origin,adopted_at,adopted_by)
             VALUES (9,NULL,3,'legacy-fileserver','12','poweredOn','10.20.30.5',2,4096,200,'Ubuntu 22.04 LTS',?,?,'linux',?,?,NULL,?,'adopted',?,1)""",
          (ts(minutes=2), ts(days=20), ts(days=20), ts(days=20), 480 * 3600, ts(days=20)))

c.execute("INSERT INTO vm_snapshots (vm_id,snapshot_ref,name,description,created_at) VALUES (1,'snapshot-501','pre-upgrade','Before nginx 1.26 upgrade',?)", (ts(days=5),))
c.execute("INSERT INTO vm_snapshots (vm_id,snapshot_ref,name,description,created_at) VALUES (1,'snapshot-517','post-hardening','After Security Hardening action',?)", (ts(days=2, hours=3),))
c.execute("INSERT INTO vm_snapshots (vm_id,snapshot_ref,name,description,created_at) VALUES (4,'snapshot-520','nightly','Nightly automated snapshot',?)", (ts(hours=9),))


def action_id(name):
    r = c.execute("SELECT id, script FROM actions WHERE name=?", (name,)).fetchone()
    return r if r else (None, "")


execs = [
    (1, "Update System Packages", "completed", 0, 2, "Hit:1 http://archive.ubuntu.com/ubuntu noble InRelease\nReading package lists... Done\nBuilding dependency tree... Done\n14 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\nSetting up linux-image-generic (6.8.0-45.45) ...\nProcessing triggers for initramfs-tools ...\n✓ System packages updated"),
    (1, "Security Hardening", "completed", 0, 2, "Disabling root SSH login... done\nDisabling password authentication... done\nInstalling ufw... done\nAllowing 22/tcp, 80/tcp, 443/tcp... done\nInstalling fail2ban... done\nfail2ban active (sshd jail enabled)\n✓ Hardening complete"),
    (6, "Install Docker", "completed", 0, 7, "Executing docker install script, commit: 6d9743e\n+ sh -c apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-compose-plugin\nClient: Docker Engine - Community\n Version: 27.3.1\n✓ Docker installed and enabled"),
    (4, "Collect VM Info", "completed", 0, 1, "Hostname: db-01\nOS: Rocky Linux 9.4 (Blue Onyx)\nKernel: 5.14.0-427.el9.x86_64\nCPU: 8 vCPU\nMemory: 31Gi total, 9.2Gi used\nDisk: /dev/sda2 200G (38% used)\nIP: 10.20.10.31/24\nUptime: 29 days"),
    (7, "Network Connectivity Validation", "failed", 1, 0, "Gateway 10.20.20.1 ........ PASS\nDNS resolution ............ PASS\nNTP (pool.ntp.org) ........ FAIL (timeout)\nOutbound HTTPS ............ PASS\n\n1 check failed"),
]
for k, (vm, name, status, code, days_ago, out) in enumerate(execs, start=1):
    aid, script = action_id(name)
    if isinstance(script, bytes):
        script = script.decode()
    start = ts(days=days_ago, hours=1)
    end = (now - timedelta(days=days_ago, hours=1) + timedelta(seconds=48 + 11 * k)).strftime("%Y-%m-%d %H:%M:%S")
    c.execute("""INSERT INTO action_executions (id,vm_id,action_id,action_name,script,status,exit_code,output,started_at,completed_at,created_by,created_at)
                 VALUES (?,?,?,?,?,?,?,?,?,?,1,?)""", (k, vm, aid, name, script or "#!/bin/bash", status, code, out, start, end, start))

notes = [
    ("success", "Deployment completed", "ci-runner-01 is ready at 10.20.20.15", "/vms/6", "deploy.completed", 0, ts(days=2, hours=1)),
    ("error", "Deployment failed", "staging-app-04: insufficient datastore space", "/history", "deploy.failed", 0, ts(days=1)),
    ("success", "Action completed", "Security Hardening finished on web-01 (exit 0)", "/vms/1", "execution.completed", 1, ts(days=2, hours=3)),
    ("info", "Template rebuilt", "ubuntu-24.04-cloudinit v3 built from the latest ISO", "/templates", "build.completed", 1, ts(days=6)),
]
for (lvl, title, body, link, ev, read, at) in notes:
    c.execute("INSERT INTO notifications (user_id,level,title,body,link,event,is_read,created_at,read_at) VALUES (1,?,?,?,?,?,?,?,?)",
              (lvl, title, body, link, ev, read, at, at if read else None))

c.commit()
print("seeded", DB, "reference time", now.isoformat() + "Z")
print("targets", c.execute("select count(*) from targets").fetchone()[0],
      "templates", c.execute("select count(*) from templates").fetchone()[0],
      "vms", c.execute("select count(*) from managed_vms").fetchone()[0],
      "deployments", c.execute("select count(*) from deployments").fetchone()[0],
      "executions", c.execute("select count(*) from action_executions").fetchone()[0])
bad = c.execute("PRAGMA foreign_key_check").fetchall()
if bad:
    sys.exit(f"foreign key check failed: {bad}")
