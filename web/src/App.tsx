import { Routes, Route } from "react-router-dom";
import { WorkforceScreen } from "./screens/WorkforceScreen";

/** Exposed as workforce_mfe/App via Module Federation. Routed under
 *  /workforce/* by the shell -- uses relative routes so this component
 *  works identically mounted under a prefix (in the shell) or at /
 *  (standalone dev, see main.tsx). */
export default function App() {
  return (
    <Routes>
      <Route path="/" element={<WorkforceScreen />} />
    </Routes>
  );
}
