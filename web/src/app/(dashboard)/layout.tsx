import { SidebarNav } from "@/components/sidebar-nav";

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="flex min-h-screen">
      <aside className="w-64 border-r bg-background">
        <div className="p-4 border-b">
          <span className="text-xl font-bold">Wrappr</span>
        </div>
        <SidebarNav />
      </aside>
      <main className="flex-1 p-6">{children}</main>
    </div>
  );
}
