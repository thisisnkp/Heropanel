import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, ApiRequestError, type Deployment, type Job, type Site } from "@/lib/api";
import { Alert, Button, Field, Input, Modal, Toggle, cn } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useJobs } from "@/stores/jobs";
import { useFreeDomains } from "@/features/domains/domains";
import { isJobResult, type CreateResult, type CreateSiteInput } from "./sites";

const FREE_DOMAINS_LIST_ID = "deploy-app-free-domains";

// DeployAppModal is the single guided path behind two menu entries — "Deploy
// App" (Node/Python/PHP from GitHub) and "Deploy Single Binary Proxy App"
// (Go/C++/Rust) — since both reduce to the same backend mechanism: a site
// (type php or proxy) with deploy_mode "git", a git source, and (for
// anything but PHP) a supervised runtime. The flavor only changes the
// language choices and copy shown.
type Flavor = "git-web" | "git-binary";

type WebLang = "php" | "node" | "python";
type BinaryLang = "go" | "cpp" | "rust";

const BUILD_PLACEHOLDER: Record<BinaryLang, string> = {
  go: "go build -o app .",
  cpp: "make",
  rust: "cargo build --release",
};

// Step the orchestration stopped at, for the partial-failure UI. "done" means
// every step that flavor needed succeeded.
type FailedStep = "git" | "runtime" | "deploy" | null;

export function DeployAppModal({ flavor, onClose }: { flavor: Flavor; onClose: () => void }) {
  const navigate = useNavigate();
  const { data: freeDomains } = useFreeDomains();
  const track = useJobs((s) => s.track);

  const [name, setName] = useState("");
  const [domain, setDomain] = useState("");
  const [repoUrl, setRepoUrl] = useState("");
  const [branch, setBranch] = useState("main");
  const [priv, setPriv] = useState(false);
  const [authUsername, setAuthUsername] = useState("");
  const [token, setToken] = useState("");
  const [webLang, setWebLang] = useState<WebLang>("node");
  const [binaryLang, setBinaryLang] = useState<BinaryLang>("go");
  const [buildCommand, setBuildCommand] = useState("");
  const [startCommand, setStartCommand] = useState("");
  const [port, setPort] = useState(flavor === "git-binary" ? 8080 : 3000);

  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [failedAt, setFailedAt] = useState<FailedStep>(null);
  const [createdUID, setCreatedUID] = useState<string | null>(null);

  const isPHP = flavor === "git-web" && webLang === "php";
  const runtimeValue = flavor === "git-binary" ? (binaryLang === "go" ? "go" : "generic") : webLang;
  const needsRuntime = !isPHP;

  const title = flavor === "git-web" ? "Deploy App" : "Deploy single-binary proxy app";

  const openSite = (tab: string) => {
    onClose();
    if (createdUID) navigate(`/sites/${createdUID}?tab=${tab}`);
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    setFailedAt(null);

    // Step 1: create the site. The queue may be enabled, in which case this
    // returns a job instead of the site — the uid isn't known until it
    // finishes, so wait for it the same way the job drawer does.
    let site: Site;
    try {
      const input: CreateSiteInput = {
        name: name.trim(),
        primary_domain: domain.trim(),
        type: isPHP ? "php" : "proxy",
        deploy_mode: "git",
      };
      const res = await api.post<CreateResult>("/sites", input);
      site = isJobResult(res) ? await waitForSite(res.job.id) : res;
    } catch (err) {
      setSubmitting(false);
      setError(err instanceof ApiRequestError ? err.message : "Could not create the site.");
      return;
    }
    setCreatedUID(site.uid);

    // Step 2: git source.
    try {
      await api.put(`/sites/${site.uid}/git`, {
        repo_url: repoUrl.trim(),
        branch: branch.trim() || "main",
        build_command: buildCommand.trim(),
        auth_kind: priv ? "token" : "none",
        auth_username: priv ? authUsername.trim() : "",
        token: priv ? token : "",
        auto_composer: true,
      });
    } catch (err) {
      setSubmitting(false);
      setFailedAt("git");
      setError(err instanceof ApiRequestError ? err.message : "Could not save the Git source.");
      return;
    }

    // Step 3: runtime (skipped for PHP — PHP-FPM serves it directly).
    if (needsRuntime) {
      try {
        await api.put(`/sites/${site.uid}/runtime`, {
          runtime: runtimeValue,
          command: startCommand.trim(),
          port,
        });
      } catch (err) {
        setSubmitting(false);
        setFailedAt("runtime");
        setError(err instanceof ApiRequestError ? err.message : "Could not save the runtime.");
        return;
      }
    }

    // Step 4: first deploy. A failure here is non-fatal — everything above
    // already saved — so it's a warning, not a blocking error.
    try {
      const res = await api.post<{ job?: Job } & Deployment>(`/sites/${site.uid}/git/deploy`);
      if (res.job) track(res.job.id, "Deploy");
      else toast.success("Deployed");
    } catch (err) {
      toast.error(
        "Initial deploy failed",
        err instanceof ApiRequestError ? err.message : "Retry it from the site's Git tab.",
      );
    }

    setSubmitting(false);
    toast.success(`${title} — site created`);
    onClose();
    navigate(`/sites/${site.uid}?tab=${needsRuntime ? "runtime" : "git"}`);
  };

  if (error) {
    return (
      <Modal title={title} onClose={onClose}>
        <div className="space-y-4">
          <Alert>{error}</Alert>
          {createdUID ? (
            <p className="text-sm text-muted">
              The site was created. Finish this step by hand on its{" "}
              {failedAt === "runtime" ? "Runtime" : "Git"} tab.
            </p>
          ) : (
            <p className="text-sm text-muted">Nothing was created — fix the details and try again.</p>
          )}
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={onClose}>
              Close
            </Button>
            {createdUID ? (
              <Button onClick={() => openSite(failedAt === "runtime" ? "runtime" : "git")}>Open site</Button>
            ) : (
              <Button
                onClick={() => {
                  setError(null);
                  setFailedAt(null);
                }}
              >
                Back to form
              </Button>
            )}
          </div>
        </div>
      </Modal>
    );
  }

  return (
    <Modal title={title} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label="Name">
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Acme" />
        </Field>
        <Field label="Primary domain" hint={freeDomains?.fqdns.length ? "Pick a verified domain, or type any domain." : undefined}>
          <Input
            list={FREE_DOMAINS_LIST_ID}
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
            placeholder="app.example.com"
          />
          <datalist id={FREE_DOMAINS_LIST_ID}>
            {(freeDomains?.fqdns ?? []).map((d) => (
              <option key={d} value={d} />
            ))}
          </datalist>
        </Field>

        {flavor === "git-web" ? (
          <Field label="Language">
            <div className="flex gap-2">
              {(["node", "python", "php"] as WebLang[]).map((l) => (
                <button
                  key={l}
                  type="button"
                  onClick={() => setWebLang(l)}
                  className={cn(
                    "flex-1 rounded-lg border px-3 py-2 text-sm transition-colors",
                    webLang === l ? "border-brand bg-brand-subtle text-fg" : "border-border text-muted hover:text-fg",
                  )}
                >
                  {l === "node" ? "Node.js" : l === "python" ? "Python" : "PHP"}
                </button>
              ))}
            </div>
          </Field>
        ) : (
          <Field label="Language">
            <div className="flex gap-2">
              {(["go", "cpp", "rust"] as BinaryLang[]).map((l) => (
                <button
                  key={l}
                  type="button"
                  onClick={() => setBinaryLang(l)}
                  className={cn(
                    "flex-1 rounded-lg border px-3 py-2 text-sm transition-colors",
                    binaryLang === l ? "border-brand bg-brand-subtle text-fg" : "border-border text-muted hover:text-fg",
                  )}
                >
                  {l === "go" ? "Go" : l === "cpp" ? "C++" : "Rust"}
                </button>
              ))}
            </div>
          </Field>
        )}

        <Field label="Repository URL">
          <Input value={repoUrl} onChange={(e) => setRepoUrl(e.target.value)} placeholder="https://github.com/acme/app.git" />
        </Field>
        <div className="grid grid-cols-2 gap-4">
          <Field label="Branch">
            <Input value={branch} onChange={(e) => setBranch(e.target.value)} placeholder="main" />
          </Field>
          <div className="flex items-end justify-between rounded-lg border border-border px-3 py-2">
            <span className="text-sm text-fg">Private repo</span>
            <Toggle checked={priv} onChange={setPriv} />
          </div>
        </div>
        {priv && (
          <div className="grid grid-cols-2 gap-4">
            <Field label="Username" hint="blank picks the provider default">
              <Input value={authUsername} onChange={(e) => setAuthUsername(e.target.value)} />
            </Field>
            <Field label="Token" hint="stored sealed; never shown again">
              <Input type="password" value={token} onChange={(e) => setToken(e.target.value)} />
            </Field>
          </div>
        )}

        <Field
          label={isPHP ? "Build command (optional)" : "Build command (optional)"}
          hint={isPHP ? "composer install runs automatically when composer.json is present" : "runs after clone, before start"}
        >
          <Input
            value={buildCommand}
            onChange={(e) => setBuildCommand(e.target.value)}
            placeholder={flavor === "git-binary" ? BUILD_PLACEHOLDER[binaryLang] : webLang === "node" ? "npm ci && npm run build" : ""}
          />
        </Field>

        {needsRuntime && (
          <>
            <Field label="Start command">
              <Input
                value={startCommand}
                onChange={(e) => setStartCommand(e.target.value)}
                placeholder={flavor === "git-binary" ? "./app" : webLang === "node" ? "node server.js" : "python app.py"}
              />
            </Field>
            <Field label="Port" hint="the local port the app listens on">
              <Input type="number" value={port} onChange={(e) => setPort(Number(e.target.value))} />
            </Field>
          </>
        )}

        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={submitting}>
            Deploy
          </Button>
        </div>
        {submitting && <p className="text-right text-xs text-muted">Creating the site and deploying — this can take a minute.</p>}
      </form>
    </Modal>
  );
}

// waitForSite polls a "site.create" job to completion and returns the created
// site from its result — the same JSON the job worker marshals
// (internal/job/worker.go), which is the full *site.Site view including uid.
async function waitForSite(jobId: string): Promise<Site> {
  const start = Date.now();
  while (Date.now() - start < 60_000) {
    const j = await api.get<Job>(`/jobs/${jobId}`);
    if (j.status === "succeeded") return j.result as Site;
    if (j.status === "failed") {
      throw new ApiRequestError(500, { code: "job_failed", message: j.error || "Site creation failed" });
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new ApiRequestError(504, { code: "job_timeout", message: "Timed out waiting for the site to be created." });
}
