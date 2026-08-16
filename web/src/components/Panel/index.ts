import { Panel as PanelRoot } from "./Panel";
import { PanelHeader } from "./PanelHeader";
import { PanelTitle } from "./PanelTitle";
import { PanelCount } from "./PanelCount";
import { PanelBody } from "./PanelBody";
import { PanelFooter } from "./PanelFooter";
import { PanelTabs } from "./PanelTabs";

export const Panel = Object.assign(PanelRoot, {
  Header: PanelHeader,
  Title: PanelTitle,
  Count: PanelCount,
  Body: PanelBody,
  Footer: PanelFooter,
  Tabs: PanelTabs,
});
