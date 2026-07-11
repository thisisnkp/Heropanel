import { useState } from "react";
import { ApiRequestError } from "@/lib/api";
import { Alert, Button, Card, Field, Input } from "@/components/ui";
import { AuthShell } from "./AuthShell";
import { useLogin } from "./auth";

export function LoginPage() {
  const login = useLogin();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    login.mutate({ email, password });
  };

  const errorMessage =
    login.error instanceof ApiRequestError ? login.error.message : login.error ? "Login failed." : null;

  return (
    <AuthShell title="Sign in" subtitle="Welcome back to HeroPanel">
      <Card className="p-6">
        <form className="space-y-4" onSubmit={onSubmit}>
          {errorMessage && <Alert>{errorMessage}</Alert>}
          <Field label="Email">
            <Input
              type="email"
              autoComplete="username"
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
      </Card>
    </AuthShell>
  );
}
