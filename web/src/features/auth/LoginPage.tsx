import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiRequestError } from "@/lib/api";
import { Alert, Button, Card, Field, Input } from "@/components/ui";
import { loginPasskey, supportsWebAuthn } from "@/lib/webauthn";
import { AuthShell } from "./AuthShell";
import { useLogin } from "./auth";

export function LoginPage() {
  const login = useLogin();
  const qc = useQueryClient();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [passkeyBusy, setPasskeyBusy] = useState(false);
  const [passkeyErr, setPasskeyErr] = useState<string | null>(null);

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    login.mutate({ email, password });
  };

  const onPasskey = async () => {
    setPasskeyErr(null);
    if (!email) {
      setPasskeyErr("Enter your email first.");
      return;
    }
    setPasskeyBusy(true);
    try {
      const principal = await loginPasskey(email);
      qc.setQueryData(["me"], principal);
      qc.invalidateQueries({ queryKey: ["auth-status"] });
    } catch (e: unknown) {
      setPasskeyErr(e instanceof Error ? e.message : "Passkey sign-in failed.");
    } finally {
      setPasskeyBusy(false);
    }
  };

  const errorMessage =
    passkeyErr ??
    (login.error instanceof ApiRequestError ? login.error.message : login.error ? "Login failed." : null);

  return (
    <AuthShell title="Sign in" subtitle="Welcome back to HeroPanel">
      <Card className="p-6">
        <form className="space-y-4" onSubmit={onSubmit}>
          {errorMessage && <Alert>{errorMessage}</Alert>}
          <Field label="Email">
            <Input
              type="email"
              autoComplete="username webauthn"
              autoFocus
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@example.com"
            />
          </Field>
          <Field label="Password">
            <Input
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
            />
          </Field>
          <Button type="submit" className="w-full" loading={login.isPending}>
            Sign in
          </Button>
        </form>
        {supportsWebAuthn() && (
          <>
            <div className="my-4 flex items-center gap-3 text-xs text-muted">
              <span className="h-px flex-1 bg-border" />
              or
              <span className="h-px flex-1 bg-border" />
            </div>
            <Button variant="ghost" className="w-full" loading={passkeyBusy} onClick={onPasskey}>
              Sign in with a passkey
            </Button>
          </>
        )}
      </Card>
    </AuthShell>
  );
}
