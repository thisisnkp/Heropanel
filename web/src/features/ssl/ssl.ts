import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type Certificate } from "@/lib/api";

export function useCertificates() {
  return useQuery({ queryKey: ["ssl"], queryFn: () => api.get<Certificate[]>("/ssl/certificates") });
}

export function useIssueCert() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { domain: string; method: string; webroot?: string }) => api.post<Certificate>("/ssl/issue", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["ssl"] }),
  });
}

export function useSelfSigned() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { domain: string }) => api.post<Certificate>("/ssl/self-signed", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["ssl"] }),
  });
}

export function useUploadCert() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { cert_pem: string; key_pem: string }) => api.post<Certificate>("/ssl/upload", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["ssl"] }),
  });
}
