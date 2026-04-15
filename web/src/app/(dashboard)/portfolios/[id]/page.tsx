import { z } from "zod";

import { PortfolioDetail } from "@/components/portfolios/portfolio-detail";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

const paramsSchema = z.object({
  id: z.string().uuid(),
});

export default async function PortfolioPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  const parsed = paramsSchema.safeParse({ id });
  if (!parsed.success) {
    return (
      <div className="mx-auto max-w-2xl space-y-4">
        <Card>
          <CardHeader>
            <CardTitle>Invalid portfolio id</CardTitle>
            <CardDescription>
              The path parameter must be a UUID matching `GET /v1/portfolios/:id`.
            </CardDescription>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            Received: <span className="font-mono">{id}</span>
          </CardContent>
        </Card>
      </div>
    );
  }

  return <PortfolioDetail portfolioId={parsed.data.id} />;
}
