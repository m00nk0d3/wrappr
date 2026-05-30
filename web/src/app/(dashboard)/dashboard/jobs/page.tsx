import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export default function JobsPage() {
  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">Jobs</h1>
        <Button>New Job</Button>
      </div>
      <Card>
        <CardHeader><CardTitle>All Jobs</CardTitle></CardHeader>
        <CardContent>
          <p className="text-muted-foreground">No jobs yet. Create your first job to get started.</p>
        </CardContent>
      </Card>
    </div>
  );
}
