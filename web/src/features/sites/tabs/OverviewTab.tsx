import type { Site } from "@/lib/api";
import { Card } from "@/components/ui";
import { InfoRow } from "../SiteDetailPage";

export function OverviewTab({ site }: { site: Site }) {
  return (
    <Card className="p-5">
      <InfoRow label="Primary domain">{site.primary_domain}</InfoRow>
      <InfoRow label="Type">{site.type}</InfoRow>
      <InfoRow label="Deploy mode">{site.deploy_mode}</InfoRow>
      <InfoRow label="Web server">{site.webserver}</InfoRow>
      <InfoRow label="System user">
        <code className="rounded bg-surface px-1.5 py-0.5 text-xs">{site.system_user}</code>
      </InfoRow>
      <InfoRow label="Document root">
        <code className="break-all rounded bg-surface px-1.5 py-0.5 text-xs">{site.document_root}</code>
      </InfoRow>
      <InfoRow label="Created">{new Date(site.created_at).toLocaleString()}</InfoRow>
    </Card>
  );
}
