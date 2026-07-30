import { useQuery } from "@tanstack/react-query";
import { rawFetch } from "@/lib/api";
import type { OpenApiSpec } from "./openapi";

// The OpenAPI document is served raw (not inside the standard { data } envelope)
// and needs no auth, so it is fetched with rawFetch and read directly rather than
// through the enveloped api client.
export function useOpenApiSpec() {
  return useQuery({
    queryKey: ["openapi-spec"],
    queryFn: async () => {
      const res = await rawFetch("GET", "/openapi.json");
      return (await res.json()) as OpenApiSpec;
    },
    staleTime: 5 * 60 * 1000,
  });
}
