// @refresh reload
import { Suspense } from "solid-js";
import { Router } from "@solidjs/router";
import { FileRoutes } from "@solidjs/start/router";

export default function App() {
  return (
    <Router>
      <Suspense>
        <FileRoutes />
      </Suspense>
    </Router>
  );
}
