import { useState } from "react";
import { ApiRequestError } from "@/lib/api";
import { Alert, Button, Card, Field, Input } from "@/components/ui";
import { AuthShell } from "./AuthShell";
import { useBootstrap, useLogin } from "./auth";

// First-run: create the administrator, then log them straight in.
export function BootstrapPage() {
  const bootstrap = useBootstrap();
  const login = useLogin();
  const [email, setEmail] = useState("");
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    await bootstrap.mutateAsync({ email, username, password });
    await login.mutateAsync({ email, password });
  };

  const err = bootstrap.error ?? login.error;
  const errorMessage = err instanceof ApiRequestError ? err.message : err ? "Setup failed." : null;
  const pending = bootstrap.isPending || login.isPending;

  return (
    <AuthShell title="Welcome to HeroPanel" subtitle="Create the first administrator account">
      <Card className="p-6">
        <form className="space-y-4" onSubmit={onSubmit}>
          {errorMessage && <Alert>{errorMessage}</Alert>}
          <Field label="Email">
            <Input type="email" autoFocus required value={email} onChange={(e) => setEmail(e.target.value)} placeholder="admin@example.com" />
          </Field>
          <Field label="Username">
            <Input required value={username} onChange={(e) => setUsername(e.target.value)} />
          </Field>
          <Field label="Password" hint="At least 8 characters.">
            <Input type="password" autoComplete="new-password" required minLength={8} value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <Button type="submit" className="w-full" loading={pending}>
            Create administrator
          </Button>
        </form>
      </Card>
    </AuthShell>
  );
}
