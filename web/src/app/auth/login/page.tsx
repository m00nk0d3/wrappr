import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export default function LoginPage() {
  return (
    <Card className="w-full max-w-sm">
      <CardHeader><CardTitle>Sign in to Wrappr</CardTitle></CardHeader>
      <CardContent className="space-y-4">
        <Input type="email" placeholder="Email" />
        <Input type="password" placeholder="Password" />
        <Button className="w-full">Sign In</Button>
      </CardContent>
    </Card>
  );
}
