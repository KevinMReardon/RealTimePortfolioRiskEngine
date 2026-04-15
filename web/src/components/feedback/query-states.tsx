import { ApiError } from "@/lib/api/errors";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";

export function LoadingCardGrid() {
  return (
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
      {Array.from({ length: 4 }).map((_, i) => (
        <Skeleton key={i} className="h-28 w-full" />
      ))}
    </div>
  );
}

export function ErrorAlert({ error }: { error: unknown }) {
  const api = error instanceof ApiError ? error : null;
  return (
    <Alert variant="destructive">
      <AlertTitle>{api?.body?.error_code ?? "Request failed"}</AlertTitle>
      <AlertDescription className="space-y-2">
        <div>{error instanceof Error ? error.message : "Unknown error"}</div>
        {api?.body?.request_id ? (
          <div className="text-xs opacity-90">
            Request ID:{" "}
            <span className="font-mono">{api.body.request_id}</span>
          </div>
        ) : null}
      </AlertDescription>
    </Alert>
  );
}
