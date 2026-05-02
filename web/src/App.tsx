import { Router } from "wouter";
import { Content } from "./components/Content";
import { Sidebar } from "./components/Sidebar";
import { StatusBar } from "./components/StatusBar";
import { TopBar } from "./components/TopBar";
import { EventStreamProvider, QueryProvider, SelectionProvider } from "./providers";

export function App() {
  return (
    <Router base="/console">
      <QueryProvider>
        <EventStreamProvider>
          <SelectionProvider>
            <TopBar />
            <main className="grid grid-cols-[280px_minmax(0,1fr)] gap-4 px-4 py-3.5 min-h-0 overflow-hidden">
              <Sidebar />
              <Content />
            </main>
            <StatusBar />
          </SelectionProvider>
        </EventStreamProvider>
      </QueryProvider>
    </Router>
  );
}
