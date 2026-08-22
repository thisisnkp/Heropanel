<script setup lang="ts">
/**
 * Sign in, and — when the account has one — the second factor.
 *
 * Both steps live in one screen rather than two routes because the MFA token is
 * held in memory: navigating away would drop it and send the operator back to
 * re-entering a password they just entered correctly.
 */
import { computed, nextTick, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useSessionStore } from "@/stores/session";
import { ApiRequestError } from "@/lib/api";
import AuthShell from "./AuthShell.vue";

const session = useSessionStore();
const router = useRouter();
const route = useRoute();

const email = ref("");
const password = ref("");
const code = ref("");
const busy = ref(false);
const error = ref<string | null>(null);
const codeInput = ref<HTMLInputElement | null>(null);

const awaitingCode = computed(() => session.mfaToken !== null);

/**
 * Where to land after signing in.
 *
 * `?next=` is only honoured when it is a path on this origin. A login screen
 * that forwards to whatever a query parameter says is an open redirect, and
 * it is worth exactly nothing to us — the operator arrived here from inside the
 * panel or from the address bar.
 */
function destination(): string {
  const next = route.query.next;
  if (typeof next === "string" && next.startsWith("/") && !next.startsWith("//")) return next;
  return "/";
}

async function submit() {
  busy.value = true;
  error.value = null;
  try {
    const res = await session.login(email.value.trim(), password.value);
    if (res === "mfa") {
      password.value = "";
      await nextTick();
      codeInput.value?.focus();
      return;
    }
    await router.replace(destination());
  } catch (e) {
    error.value = message(e);
  } finally {
    busy.value = false;
  }
}

async function submitCode() {
  busy.value = true;
  error.value = null;
  try {
    await session.completeMFA(code.value.trim());
    await router.replace(destination());
  } catch (e) {
    error.value = message(e);
    code.value = "";
  } finally {
    busy.value = false;
  }
}

/**
 * Bad credentials get one deliberately vague sentence.
 *
 * npd already refuses to say whether the address or the password was wrong —
 * saying so turns the login form into an account-enumeration oracle. Anything
 * that is *not* a rejection (the panel is unreachable, rate limiting) does get
 * its real message, because that is a problem the operator can act on.
 */
function message(e: unknown): string {
  if (e instanceof ApiRequestError) {
    if (e.status === 401) return "That email address and password do not match an account.";
    return e.message;
  }
  return "Sign-in failed.";
}
</script>

<template>
  <AuthShell
    :title="awaitingCode ? 'Two-factor code' : 'Sign in'"
    :subtitle="awaitingCode ? 'Enter the six-digit code from your authenticator app.' : undefined"
  >
    <NxCallout v-if="error" tone="danger">{{ error }}</NxCallout>

    <form v-if="!awaitingCode" class="login__form" @submit.prevent="submit">
      <NxField label="Email">
        <template #default="{ id }">
          <NxInput
            :id="id"
            v-model="email"
            type="email"
            placeholder="you@example.com"
            autocomplete="username"
            required
          />
        </template>
      </NxField>

      <NxField label="Password">
        <template #default="{ id }">
          <NxInput
            :id="id"
            v-model="password"
            type="password"
            placeholder="Password"
            autocomplete="current-password"
            required
          />
        </template>
      </NxField>

      <NxButton type="submit" variant="primary" size="lg" :loading="busy">Sign in</NxButton>
    </form>

    <form v-else class="login__form" @submit.prevent="submitCode">
      <NxField label="Authentication code">
        <template #default="{ id }">
          <NxInput
            :id="id"
            ref="codeInput"
            v-model="code"
            mono
            placeholder="000000"
            inputmode="numeric"
            autocomplete="one-time-code"
            required
          />
        </template>
      </NxField>

      <NxButton type="submit" variant="primary" size="lg" :loading="busy">Verify</NxButton>
    </form>
  </AuthShell>
</template>

<style scoped>
.login__form { display: flex; flex-direction: column; gap: 16px; }
</style>
