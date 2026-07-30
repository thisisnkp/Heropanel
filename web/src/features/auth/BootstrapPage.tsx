import { useState } from "react";
import { ApiRequestError } from "@/lib/api";
import { Alert, Button, Card, Field, Input } from "@/components/ui";
import { useT } from "@/lib/i18n";
import { AuthShell } from "./AuthShell";
import { useBootstrap, useLogin } from "./auth";

// First-run: create the administrator, then log them straight in.
export function BootstrapPage() {
  const t = useT();
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
  const errorMessage = err instanceof ApiRequestError ? err.message : err ? t("auth.bootstrap.failed") : null;
  const pending = bootstrap.isPending || login.isPending;

  return (
    <AuthShell title={t("auth.bootstrap.title")} subtitle={t("auth.bootstrap.subtitle")}>
      <Card className="p-6">
        <form className="space-y-4" onSubmit={onSubmit}>
          {errorMessage && <Alert>{errorMessage}</Alert>}
          <Field label={t("auth.field.email")}>
            <Input type="email" autoFocus required value={email} onChange={(e) => setEmail(e.target.value)} placeholder="admin@example.com" />
          </Field>
          <Field label={t("auth.field.username")}>
            <Input required value={username} onChange={(e) => setUsername(e.target.value)} />
          </Field>
          <Field label={t("auth.field.password")} hint={t("auth.bootstrap.passwordHint")}>
            <Input type="password" autoComplete="new-password" required minLength={8} value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <Button type="submit" className="w-full" loading={pending}>
            {t("auth.bootstrap.submit")}
          </Button>
        </form>
      </Card>
    </AuthShell>
  );
}
