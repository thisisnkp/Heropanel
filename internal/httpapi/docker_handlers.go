package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/docker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// The Docker HTTP edge.
//
// Two permissions, split the way the risk splits: `docker.read` sees containers,
// logs and stats; `docker.write` starts, stops, restarts, removes, and pulls.
// They are not folded into `site.write` — being able to edit a site is a much
// smaller grant than being able to stop the container serving it, and the
// container view is host-wide rather than scoped to one site.
//
// Ownership is *not* checked here. The broker refuses any container the panel
// did not create, and that check belongs in the privileged process where it
// cannot be skipped by a new route forgetting to call it.

// dockerInfoHandler reports whether the host has a usable daemon. Gated by
// "docker.read". It never fails for an absent daemon: the UI needs to render
// "Docker is not installed" as a state, not an error page.
func dockerInfoHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, d.Docker.Info(r.Context()))
	}
}

// listContainersHandler lists containers, optionally scoped to a site via the
// `site` query parameter. Gated by "docker.read".
func listContainersHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Docker.ListContainers(r.Context(), r.URL.Query().Get("site"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// inspectContainerHandler returns docker's own inspect payload. Gated by
// "docker.read".
func inspectContainerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Docker.Inspect(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// containerLogsHandler returns a bounded tail of a container's output. Gated by
// "docker.read" and force-audited: container logs routinely carry connection
// strings, tokens and customer data, so reading them is a disclosure worth the
// same log line a file download gets.
func containerLogsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		audit.Force(r.Context())
		audit.AddDetail(r.Context(), "container", id)

		tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
		out, err := d.Docker.Logs(r.Context(), id,
			docker.ClampTail(tail), r.URL.Query().Get("timestamps") == "true")
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// containerStatsHandler samples resource usage once. Gated by "docker.read".
// One sample per request: the client polls, so a slow or dead container can
// never hold a request open the way a stream would.
func containerStatsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Docker.Stats(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// containerActionHandler starts, stops, restarts or removes a container. Gated
// by "docker.write".
func containerActionHandler(d Deps, verb string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		audit.AddDetail(r.Context(), "container", id)
		audit.AddDetail(r.Context(), "action", verb)

		force := r.URL.Query().Get("force") == "true"
		if err := d.Docker.Action(r.Context(), verb, id, force); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "container": id})
	}
}

// listImagesHandler lists images on the host. Gated by "docker.read".
func listImagesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Docker.ListImages(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// pullImageHandler fetches an image. Gated by "docker.write": pulling runs
// someone else's code onto the host the moment it is started, and it consumes
// disk, so it is not a read.
func pullImageHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Image string `json:"image"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Image == "" {
			writeError(w, r, errx.Validation("image_required", "An image reference is required."))
			return
		}
		audit.AddDetail(r.Context(), "image", req.Image)

		log, err := d.Docker.PullImage(r.Context(), req.Image)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"image": req.Image, "log": log})
	}
}

// removeImageHandler deletes an image. Gated by "docker.write". The broker
// passes docker's "still used by a container" refusal straight through, so this
// cannot orphan a running app; `force` only detaches extra tags.
func removeImageHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := chi.URLParam(r, "ref")
		audit.AddDetail(r.Context(), "image", ref)
		force := r.URL.Query().Get("force") == "true"
		log, err := d.Docker.RemoveImage(r.Context(), ref, force)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "image": ref, "log": log})
	}
}

// pruneImagesHandler reclaims disk from unused images. Gated by "docker.write".
// Dangling-only by default; `all=true` extends it to every image no container
// uses — a bigger hammer, so it is opt-in.
func pruneImagesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all := r.URL.Query().Get("all") == "true"
		audit.AddDetail(r.Context(), "all", strconv.FormatBool(all))
		log, err := d.Docker.PruneImages(r.Context(), all)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "log": log})
	}
}

// createContainerHandler starts a container. Gated by "docker.write".
//
// The request is passed to the service as a typed spec, not as docker flags.
// Every hardening rule — named volumes only, loopback-only publishing, the
// restart-policy allowlist — lives in the broker, so a caller that constructs
// this request by hand gets exactly the same refusals the UI does.
func createContainerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var spec docker.ContainerSpec
		if !decodeJSON(w, r, &spec) {
			return
		}
		audit.AddDetail(r.Context(), "container", spec.Name)
		audit.AddDetail(r.Context(), "image", spec.Image)
		// Deliberately not audited: the environment. It is where a generated
		// database password lives, and an audit log is not the place to copy one.

		id, err := d.Docker.CreateContainer(r.Context(), spec)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, map[string]any{"name": spec.Name, "id": id})
	}
}

// listVolumesHandler lists volumes. Gated by "docker.read".
func listVolumesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Docker.ListVolumes(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// inspectVolumeHandler returns a volume's detail and the containers that mount
// it. Gated by "docker.read": it is what makes the destructive remove an
// informed decision rather than a guess.
func inspectVolumeHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Docker.InspectVolume(r.Context(), chi.URLParam(r, "name"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// createVolumeHandler creates a named volume. Gated by "docker.write".
func createVolumeHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
			Site string `json:"site"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.AddDetail(r.Context(), "volume", req.Name)
		if err := d.Docker.CreateVolume(r.Context(), req.Name, req.Site); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, map[string]any{"name": req.Name})
	}
}

// removeVolumeHandler deletes a volume and its contents. Gated by
// "docker.write" and refused by the broker for volumes the panel does not own —
// this is the one operation in the module that destroys data.
func removeVolumeHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		audit.AddDetail(r.Context(), "volume", name)
		if err := d.Docker.RemoveVolume(r.Context(), name); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "name": name})
	}
}

// listNetworksHandler lists networks. Gated by "docker.read".
func listNetworksHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Docker.ListNetworks(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// inspectNetworkHandler returns a network's detail, including its connected
// containers. Gated by "docker.read".
func inspectNetworkHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Docker.InspectNetwork(r.Context(), chi.URLParam(r, "name"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// createNetworkHandler creates a bridge network. Gated by "docker.write".
func createNetworkHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
			Site string `json:"site"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.AddDetail(r.Context(), "network", req.Name)
		if err := d.Docker.CreateNetwork(r.Context(), req.Name, req.Site); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, map[string]any{"name": req.Name})
	}
}

// removeNetworkHandler deletes a network. Gated by "docker.write".
func removeNetworkHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		audit.AddDetail(r.Context(), "network", name)
		if err := d.Docker.RemoveNetwork(r.Context(), name); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "name": name})
	}
}

// containerExecHandler upgrades to a WebSocket and runs a shell inside a
// container. Gated by "docker.write" and force-audited.
//
// The permission is `docker.write`, not `docker.read`: a shell inside a
// container can stop the process, edit its data and read its secrets, which is
// strictly more than the lifecycle buttons offer. It is not a separate
// permission from `docker.write` either, because anyone who can already remove
// the container can do more damage than a shell in it.
//
// It reuses the terminal's pumps verbatim — same framing, same recorder-shaped
// stream — because a container shell and a site shell are the same problem.
func containerExecHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		shell := r.URL.Query().Get("shell")
		cols := uint16(termDimension(r.URL.Query().Get("cols"), 0))
		rows := uint16(termDimension(r.URL.Query().Get("rows"), 0))

		audit.Force(r.Context())
		audit.AddDetail(r.Context(), "container", id)

		// Opened before the upgrade so a refusal — no broker, an unmanaged
		// container, policy — is a clean JSON error instead of an opaque close.
		stream, err := d.Docker.OpenExec(r.Context(), id, shell, cols, rows)
		if err != nil {
			writeError(w, r, err)
			return
		}
		defer func() { _ = stream.Close() }()

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
		if err != nil {
			return // Accept already responded
		}
		defer func() { _ = conn.CloseNow() }()
		conn.SetReadLimit(1 << 20)

		ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
		defer cancel()

		// nil recorder: container sessions are audited (who opened a shell in
		// which container) but not transcribed. Recording is tied to a site's
		// retention and permissions, and a container shell has no site to hang
		// that on — inventing one here would put transcripts somewhere nothing
		// sweeps. Noted as deferred in docs/19.
		go pumpBrokerToWS(ctx, cancel, conn, stream, nil)
		pumpWSToBroker(ctx, conn, stream, nil)
	}
}

// containerLogsStreamHandler upgrades to a WebSocket and follows a container's
// logs live. Gated by "docker.read" — it is the same disclosure as the polled
// logs read, just pushed instead of pulled — and force-audited for the same
// reason: logs routinely carry connection strings and tokens.
//
// It reuses the terminal's pumps with a nil recorder. The broker → client half
// carries the log output and the final exit frame; the client → broker half
// exists only to notice the browser closing the tab, which stops the follow.
func containerLogsStreamHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))

		audit.Force(r.Context())
		audit.AddDetail(r.Context(), "container", id)

		// Opened before the upgrade so a refusal is a clean JSON error rather than
		// an opaque WebSocket close.
		stream, err := d.Docker.OpenLogStream(r.Context(), id, tail, r.URL.Query().Get("timestamps") == "true")
		if err != nil {
			writeError(w, r, err)
			return
		}
		defer func() { _ = stream.Close() }()

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		conn.SetReadLimit(1 << 20)

		ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
		defer cancel()

		go pumpBrokerToWS(ctx, cancel, conn, stream, nil)
		pumpWSToBroker(ctx, conn, stream, nil)
	}
}

// composeUpHandler brings a compose stack up from a submitted compose file.
// Gated by "docker.write".
//
// A compose file is user-authored YAML and the honest boundary is stated in the
// broker: the panel labels and scopes the stack but cannot harden arbitrary
// compose the way it hardens a container it builds. This is the module's
// explicit escape hatch, and it is documented as one.
func composeUpHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Project string `json:"project"`
			Site    string `json:"site"`
			File    string `json:"file"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.AddDetail(r.Context(), "project", req.Project)
		log, err := d.Docker.ComposeUp(r.Context(), req.Project, req.Site, req.File)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, map[string]any{"project": req.Project, "log": log})
	}
}

// composeDownHandler tears a stack down. Gated by "docker.write". Refused by the
// broker for stacks the panel did not create, and never removes volumes.
func composeDownHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project := chi.URLParam(r, "project")
		audit.AddDetail(r.Context(), "project", project)
		if err := d.Docker.ComposeDown(r.Context(), project); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "project": project})
	}
}

// composeStatusHandler lists a stack's services. Gated by "docker.read".
func composeStatusHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Docker.ComposePs(r.Context(), chi.URLParam(r, "project"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// composeLogsHandler returns a bounded tail of a stack's logs. Gated by
// "docker.read" and force-audited.
func composeLogsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project := chi.URLParam(r, "project")
		audit.Force(r.Context())
		audit.AddDetail(r.Context(), "project", project)
		tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
		out, err := d.Docker.ComposeLogs(r.Context(), project, docker.ClampTail(tail))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}
