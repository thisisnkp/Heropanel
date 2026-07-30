import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type Webhook = {
  uid: string;
  url: string;
  events: string[];
  active: boolean;
  created_at: string;
};

export type CreatedWebhook = Webhook & { secret: string };

export type WebhookDelivery = {
  uid: string;
  event: string;
  resource_type: string;
  resource_id: string;
  status: string;
  attempts: number;
  response_code: number;
  error: string;
  created_at: string;
  delivered_at: string;
};

export function useWebhooks() {
  return useQuery({
    queryKey: ["webhooks"],
    queryFn: () => api.get<{ webhooks: Webhook[] }>("/webhooks"),
  });
}

export function useCreateWebhook() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { url: string; events: string[] }) => api.post<CreatedWebhook>("/webhooks", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["webhooks"] }),
  });
}

export function useDeleteWebhook() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.del(`/webhooks/${uid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["webhooks"] }),
  });
}

export function useWebhookDeliveries(uid: string, enabled: boolean) {
  return useQuery({
    queryKey: ["webhook-deliveries", uid],
    queryFn: () => api.get<{ deliveries: WebhookDelivery[] }>(`/webhooks/${uid}/deliveries`),
    enabled,
  });
}
