<script setup lang="ts">
/**
 * Creating the first administrator, on a panel that has none.
 *
 * This screen exists exactly once per installation and then becomes
 * unreachable: npd refuses /auth/bootstrap the moment an account exists, so it
 * is not a "create user" form that happens to be public.
 *
 * The password rules are shown before they are broken rather than after. A
 * server-side rejection is still the authority — this only spares the operator
 * a round trip on the one form where a rejection is most annoying, because
 * everything they typed is new.
 */
import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import { useSessionStore } from "@/stores/session";
import { ApiRequestError } from "@/lib/api";
import AuthShell from "./AuthShell.vue";

const session = useSessionStore();
const router = useRouter();

const email = ref("");
const username = ref("");
const password = ref("");
const confirm = ref("");
const busy = ref(false);
const error = ref<string | null>(null);

const MIN = 12;

const tooShort = computed(() => password.value.length > 0 && password.value.length < MIN);
const mismatch = computed(() => confirm.value.length > 0 && confirm.value !== password.value);
const ready = computed(
  () =>
    email.value.trim() !== "" &&
    username.value.trim() !== "" &&
    password.value.length >= MIN &&
    confirm.value === password.value,
);

async function submit() {
  if (!ready.value) return;
  busy.value = true;
  error.value = null;
  try {
    await session.bootstrap(email.value.trim(), username.value.trim(), password.value);
    await router.replace("/");
  } catch (e) {
    error.value = e instanceof ApiRequestError ? e.message : "Could not create the administrator.";
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <AuthShell
    title="Create the administrator"
    subtitle="This panel has no accounts yet. The one you make here owns it."
  >
    <NxCallout v-if="error" tone="danger">{{ error }}</NxCallout>

    <form class="boot__form" @submit.prevent="submit">
      <NxField label="Email">
        <template #default="{ id }">
          <NxInput :id="id" v-model="email" type="email" placeholder="you@example.com" autocomplete="username" required />
        </template>
      </NxField>

      <NxField label="Username">
        <template #default="{ id }">
          <NxInput :id="id" v-model="username" placeholder="admin" autocomplete="username" required />
        </template>
      </NxField>

      <NxField
        label="Password"
        :hint="`At least ${MIN} characters.`"
        :error="tooShort ? `Use at least ${MIN} characters.` : undefined"
      >
        <template #default="{ id, invalid }">
          <NxInput
            :id="id"
            v-model="password"
            type="password"
            placeholder="Password"
            autocomplete="new-password"
            :invalid="invalid"
            required
          />
        </template>
      </NxField>

      <NxField label="Confirm password" :error="mismatch ? 'The two passwords do not match.' : undefined">
        <template #default="{ id, invalid }">
          <NxInput
            :id="id"
            v-model="confirm"
            type="password"
            placeholder="Password"
            autocomplete="new-password"
            :invalid="invalid"
            required
          />
        </template>
      </NxField>

      <NxButton type="submit" variant="primary" size="lg" :loading="busy" :disabled="!ready">
        Create administrator
      </NxButton>
    </form>

    <template #footer>
      There is no password reset on a panel with one account and no mail configured yet. Store this
      one somewhere you can get it back from.
    </template>
  </AuthShell>
</template>

<style scoped>
.boot__form { display: flex; flex-direction: column; gap: 16px; }
</style>
