import { computed, ref } from "vue";
import { defineStore } from "pinia";

export interface SessionUser {
  readonly id: string;
  readonly name: string;
  readonly email: string;
  readonly permissions: readonly string[];
}

/**
 * Who is signed in, and what they may do.
 *
 * `can()` is the single permission check the UI uses. It mirrors npd's RBAC
 * names (`site.write`, `system.read`, …) so a screen hidden here is a screen the
 * API would refuse anyway — the check is a courtesy, never the enforcement.
 */
export const useSessionStore = defineStore("session", () => {
  const user = ref<SessionUser | null>(null);
  const loading = ref(true);

  const isAuthenticated = computed(() => user.value !== null);

  function can(permission: string) {
    return user.value?.permissions.includes(permission) ?? false;
  }

  function set(next: SessionUser | null) {
    user.value = next;
    loading.value = false;
  }

  return { user, loading, isAuthenticated, can, set };
});
