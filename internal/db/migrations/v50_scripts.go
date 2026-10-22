package migrations

// v50UpdateDockerContainersScript pulls newer images for every running
// container and recreates what it safely can. Compose-managed containers are
// updated through "docker compose pull / up -d" in their own project
// directory, so they come back with exactly the configuration their compose
// file describes. A container started by hand ("docker run") has no such
// source of truth, so by default the script only pulls its image and reports
// that it needs recreating; STANDALONE=recreate hands those to a one-shot
// Watchtower run, which recreates them with their current configuration.
var v50UpdateDockerContainersScript = `#!/bin/bash
set -euo pipefail

echo "=== Update Docker Containers ==="
echo ""

STANDALONE="${PARAM_STANDALONE:-report}"
PRUNE="${PARAM_PRUNE:-false}"
DRY_RUN="${PARAM_DRY_RUN:-false}"

case "$STANDALONE" in report|recreate) ;; *) echo "ERROR: STANDALONE must be report or recreate (got '$STANDALONE')"; exit 1;; esac

if ! command -v docker &>/dev/null; then
    echo "ERROR: Docker isn't installed. Run the 'Install Docker' action first."
    exit 1
fi
if ! docker info &>/dev/null; then
    echo "ERROR: the Docker daemon isn't running or isn't reachable (docker info failed)."
    exit 1
fi

HAS_COMPOSE=false
if docker compose version &>/dev/null; then HAS_COMPOSE=true; fi

echo "Docker:  $(docker --version)"
if [ "$HAS_COMPOSE" = true ]; then
    echo "Compose: $(docker compose version --short 2>/dev/null || docker compose version)"
else
    echo "Compose: not installed (compose-managed containers will be pulled and reported only)"
fi
if [ "$DRY_RUN" = true ]; then
    echo "Mode:    dry run - pull images and report, restart nothing"
else
    echo "Mode:    update"
fi
echo ""

# One row per running container: name|image|compose project|project dir|compose files|service
declare -A proj_dir proj_files proj_services
declare -A before_img
standalone=()
total=0
while IFS='|' read -r name image project workdir files service; do
    [ -n "$name" ] || continue
    total=$((total + 1))
    before_img["$name"]=$(docker inspect -f '{{.Image}}' "$name" 2>/dev/null || echo "")
    if [ -n "$project" ] && [ "$HAS_COMPOSE" = true ]; then
        proj_dir["$project"]="$workdir"
        proj_files["$project"]="$files"
        proj_services["$project"]="${proj_services[$project]:-} $service"
    else
        standalone+=("$name|$image")
    fi
done < <(docker ps --format '{{.Names}}|{{.Image}}|{{.Label "com.docker.compose.project"}}|{{.Label "com.docker.compose.project.working_dir"}}|{{.Label "com.docker.compose.project.config_files"}}|{{.Label "com.docker.compose.service"}}')

if [ "$total" -eq 0 ]; then
    echo "No running containers. Nothing to update."
    exit 0
fi
echo "Running containers: $total (${#proj_dir[@]} compose project(s), ${#standalone[@]} standalone)"
echo ""

updated=0; current=0; pending=0; skipped=0; failed=0
pending_names=()

# --- Compose projects -------------------------------------------------------
if [ "${#proj_dir[@]}" -gt 0 ]; then
    for project in "${!proj_dir[@]}"; do
        dir="${proj_dir[$project]}"
        files="${proj_files[$project]}"
        # shellcheck disable=SC2086
        read -ra services <<< "${proj_services[$project]}"
        echo ">>> Compose project '$project' (${dir:-no working dir}) - services: ${services[*]}"
        if [ -z "$dir" ] || [ ! -d "$dir" ]; then
            echo "    SKIP: the project directory isn't on this host (deployed from elsewhere?); pull and recreate it where the compose file lives."
            skipped=$((skipped + ${#services[@]}))
            echo ""
            continue
        fi
        args=(--project-name "$project" --project-directory "$dir")
        IFS=',' read -ra flist <<< "$files"
        for f in "${flist[@]}"; do
            [ -n "$f" ] && args+=(-f "$f")
        done
        if ! docker compose "${args[@]}" pull --quiet "${services[@]}" 2>&1 | sed 's/^/    /'; then
            echo "    FAIL: pull failed"
            failed=$((failed + ${#services[@]}))
            echo ""
            continue
        fi
        if [ "$DRY_RUN" = true ]; then
            for svc in "${services[@]}"; do
                cname=$(docker ps --filter "label=com.docker.compose.project=$project" --filter "label=com.docker.compose.service=$svc" --format '{{.Names}}' | head -1)
                image=$(docker inspect -f '{{.Config.Image}}' "$cname" 2>/dev/null || echo "")
                latest=$(docker image inspect -f '{{.Id}}' "$image" 2>/dev/null || echo "")
                if [ -n "$latest" ] && [ "$latest" != "${before_img[$cname]:-}" ]; then
                    echo "    $svc ($cname): newer image pulled - would recreate"
                    pending=$((pending + 1)); pending_names+=("$cname")
                else
                    echo "    $svc ($cname): up to date"
                    current=$((current + 1))
                fi
            done
            echo ""
            continue
        fi
        if ! docker compose "${args[@]}" up -d --no-deps "${services[@]}" 2>&1 | sed 's/^/    /'; then
            echo "    FAIL: up -d failed"
            failed=$((failed + ${#services[@]}))
            echo ""
            continue
        fi
        for svc in "${services[@]}"; do
            cname=$(docker ps -a --filter "label=com.docker.compose.project=$project" --filter "label=com.docker.compose.service=$svc" --format '{{.Names}}' | head -1)
            after=$(docker inspect -f '{{.Image}}' "$cname" 2>/dev/null || echo "")
            if [ -n "$after" ] && [ "$after" != "${before_img[$cname]:-}" ]; then
                echo "    $svc ($cname): UPDATED"
                updated=$((updated + 1))
            else
                echo "    $svc ($cname): up to date"
                current=$((current + 1))
            fi
        done
        echo ""
    done
fi

# --- Standalone containers --------------------------------------------------
if [ "${#standalone[@]}" -gt 0 ]; then
    echo ">>> Standalone containers (not managed by Compose)"
    recreate=()
    for entry in "${standalone[@]}"; do
        name="${entry%%|*}"
        image="${entry#*|}"
        if ! docker pull --quiet "$image" >/dev/null 2>&1; then
            echo "    $name ($image): FAIL - pull failed (private registry, bad tag, or no network?)"
            failed=$((failed + 1))
            continue
        fi
        latest=$(docker image inspect -f '{{.Id}}' "$image" 2>/dev/null || echo "")
        if [ -z "$latest" ] || [ "$latest" = "${before_img[$name]:-}" ]; then
            echo "    $name ($image): up to date"
            current=$((current + 1))
        elif [ "$DRY_RUN" = true ] || [ "$STANDALONE" = report ]; then
            echo "    $name ($image): newer image pulled - needs recreating"
            pending=$((pending + 1)); pending_names+=("$name")
        else
            recreate+=("$name")
        fi
    done
    if [ "${#recreate[@]}" -gt 0 ]; then
        echo ""
        echo "    Recreating ${#recreate[@]} container(s) with a one-shot Watchtower run: ${recreate[*]}"
        if docker run --rm -v /var/run/docker.sock:/var/run/docker.sock containrrr/watchtower --run-once "${recreate[@]}" 2>&1 | sed 's/^/    /'; then
            for name in "${recreate[@]}"; do
                after=$(docker inspect -f '{{.Image}}' "$name" 2>/dev/null || echo "")
                if [ -n "$after" ] && [ "$after" != "${before_img[$name]:-}" ]; then
                    echo "    $name: UPDATED"
                    updated=$((updated + 1))
                else
                    echo "    $name: FAIL - still running the old image"
                    failed=$((failed + 1))
                fi
            done
        else
            echo "    FAIL: Watchtower run failed"
            failed=$((failed + ${#recreate[@]}))
        fi
    fi
    echo ""
fi

# --- Cleanup ----------------------------------------------------------------
if [ "$PRUNE" = true ] && [ "$DRY_RUN" != true ]; then
    echo ">>> Removing dangling images"
    docker image prune -f 2>&1 | tail -1 | sed 's/^/    /'
    echo ""
fi

echo "=== Summary ==="
echo "Updated:          $updated"
echo "Already current:  $current"
if [ "$pending" -gt 0 ]; then
    echo "Need recreating:  $pending (${pending_names[*]})"
    if [ "$DRY_RUN" != true ]; then
        echo "                  Newer images are pulled; these containers weren't started by Compose, so Forgemill can't"
        echo "                  rebuild their run configuration. Re-run with STANDALONE=recreate, or recreate them by hand."
    fi
fi
[ "$skipped" -gt 0 ] && echo "Skipped:          $skipped"
[ "$failed" -gt 0 ] && echo "Failed:           $failed"
echo ""
if [ "$failed" -gt 0 ]; then
    echo "=== Update finished with failures ==="
    exit 1
fi
echo "=== Update complete ==="
`

// v50DockerContainerStatusScript is a read-only report of what Docker is
// running on the VM: engine summary, every container with image/state/ports,
// anything unhealthy or restart-looping, compose projects, a live resource
// snapshot and disk usage.
var v50DockerContainerStatusScript = `#!/bin/bash
set -euo pipefail

echo "=== Docker Container Status ==="
echo ""

INCLUDE_STOPPED="${PARAM_INCLUDE_STOPPED:-true}"
STATS="${PARAM_STATS:-true}"

if ! command -v docker &>/dev/null; then
    echo "Docker isn't installed on this VM."
    exit 0
fi
if ! docker info &>/dev/null; then
    echo "ERROR: Docker is installed but the daemon isn't running or isn't reachable (docker info failed)."
    echo "Try: systemctl status docker"
    exit 1
fi

echo "--- Engine ---"
docker info --format 'Version:    {{.ServerVersion}}
Containers: {{.Containers}} ({{.ContainersRunning}} running, {{.ContainersPaused}} paused, {{.ContainersStopped}} stopped)
Images:     {{.Images}}
Storage:    {{.Driver}}
Root dir:   {{.DockerRootDir}}'
if docker compose version &>/dev/null; then
    echo "Compose:    $(docker compose version --short 2>/dev/null || docker compose version)"
else
    echo "Compose:    not installed"
fi
echo ""

ps_args=()
if [ "$INCLUDE_STOPPED" = true ]; then
    ps_args+=(-a)
    echo "--- Containers (all) ---"
else
    echo "--- Containers (running) ---"
fi
if [ -z "$(docker ps "${ps_args[@]}" -q)" ]; then
    echo "none"
else
    docker ps "${ps_args[@]}" --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
fi
echo ""

echo "--- Attention ---"
unhealthy=$(docker ps -a --filter health=unhealthy --format '{{.Names}} ({{.Status}})')
restarting=$(docker ps -a --filter status=restarting --format '{{.Names}} ({{.Status}})')
exited=$(docker ps -a --filter status=exited --format '{{.Names}} ({{.Status}})' | grep -v 'Exited (0)' || true)
if [ -z "$unhealthy$restarting$exited" ]; then
    echo "nothing to report"
else
    [ -n "$unhealthy" ] && echo "UNHEALTHY:  $unhealthy" | sed '2,$s/^/            /'
    [ -n "$restarting" ] && echo "RESTARTING: $restarting" | sed '2,$s/^/            /'
    [ -n "$exited" ] && echo "EXITED (non-zero): $exited" | sed '2,$s/^/                   /'
fi
echo ""

echo "--- Compose projects ---"
projects=$(docker ps -a --format '{{.Label "com.docker.compose.project"}}' | grep -v '^$' | sort | uniq -c | awk '{printf "%-30s %s service(s)\n", $2, $1}')
if [ -z "$projects" ]; then
    echo "none (all containers started by hand)"
else
    echo "$projects"
fi
echo ""

if [ "$STATS" = true ] && [ -n "$(docker ps -q)" ]; then
    echo "--- Resource usage (snapshot) ---"
    docker stats --no-stream --format 'table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}\t{{.BlockIO}}'
    echo ""
fi

echo "--- Disk usage ---"
docker system df
echo ""

echo "=== Report complete ==="
`

var v50BuiltinActions = []struct {
	name, description, category, script, parameters, tags string
}{
	{
		name:        "Update Docker Containers",
		description: "Pull newer images for every running container and recreate what can be done safely: Compose-managed containers through docker compose pull / up -d in their project directory; hand-started containers are pulled and reported (or recreated via a one-shot Watchtower run when STANDALONE=recreate). Optionally prunes dangling images afterwards.",
		category:    "packages",
		script:      v50UpdateDockerContainersScript,
		parameters:  `[{"name":"STANDALONE","label":"Containers not managed by Compose","type":"select","required":false,"default":"report","placeholder":"","options":["report","recreate"],"description":"report: pull the newer image and list the container as needing recreation. recreate: hand it to a one-shot Watchtower run (pulls containrrr/watchtower from Docker Hub) which recreates it with its current configuration."},{"name":"DRY_RUN","label":"Dry run","type":"boolean","required":false,"default":"false","placeholder":"","options":null,"description":"Pull images and report what would change without restarting anything"},{"name":"PRUNE","label":"Prune dangling images","type":"boolean","required":false,"default":"false","placeholder":"","options":null,"description":"Run docker image prune -f after updating to reclaim space from the old images"}]`,
		tags:        `["docker","containers","compose","updates","maintenance"]`,
	},
	{
		name:        "Docker Container Status",
		description: "Report what Docker is running on the VM: engine summary, every container with image, state and ports, anything unhealthy or restart-looping, Compose projects, a live CPU/memory snapshot and disk usage. Read-only.",
		category:    "monitoring",
		script:      v50DockerContainerStatusScript,
		parameters:  `[{"name":"INCLUDE_STOPPED","label":"Include stopped containers","type":"boolean","required":false,"default":"true","placeholder":"","options":null,"description":"List exited and created containers as well as running ones"},{"name":"STATS","label":"Resource snapshot","type":"boolean","required":false,"default":"true","placeholder":"","options":null,"description":"Include a one-shot docker stats table (CPU, memory, network and block I/O per running container)"}]`,
		tags:        `["docker","containers","monitoring","diagnostics"]`,
	},
}
