import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export default function DashboardPage() {
  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold">Dashboard</h1>
      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader><CardTitle>Active Jobs</CardTitle></CardHeader>
          <CardContent><p className="text-2xl font-bold">—</p></CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle>Team Members</CardTitle></CardHeader>
          <CardContent><p className="text-2xl font-bold">—</p></CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle>Revenue</CardTitle></CardHeader>
          <CardContent><p className="text-2xl font-bold">—</p></CardContent>
        </Card>
      </div>
    </div>
  );
}
