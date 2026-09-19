import { ProjectsProvider } from "../../state/projects";
import { TabsProvider } from "../../state/tabs";
import { SymbolsProvider } from "../../state/symbols";
import { GitProvider } from "../../state/git";
import { DiagCounterProvider } from "../../state/diagstore";
import { AgentProvider } from "../../state/agent";
import { LayoutProvider } from "../../state/layout";
import { TerminalProvider } from "../../state/terminal";
import WorkspaceInner from "./WorkspaceInner";

declare global {
  interface Window {
    __codesaberWAt?: number;
  }
}

const Workspace: React.FC = () => {
  return (
    <ProjectsProvider>
      <SymbolsProvider>
        <TabsProvider>
          <GitProvider>
            <DiagCounterProvider>
              <AgentProvider>
                <LayoutProvider>
                  <TerminalProvider>
                    <WorkspaceInner />
                  </TerminalProvider>
                </LayoutProvider>
              </AgentProvider>
            </DiagCounterProvider>
          </GitProvider>
        </TabsProvider>
      </SymbolsProvider>
    </ProjectsProvider>
  );
};

export default Workspace;
