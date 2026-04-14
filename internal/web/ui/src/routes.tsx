import { Navigate, type RouteObject } from "react-router-dom";
import { AppShell } from "./components/layout/AppShell";
import { ChatView } from "./components/chat/ChatView";
import { HistoryView } from "./components/history/HistoryView";
import { SettingsPanel } from "./components/settings/SettingsPanel";
import { ProfilePage } from "./components/profile/ProfilePage";

export const routes: RouteObject[] = [
  {
    path: "/",
    element: <AppShell />,
    children: [
      { index: true,          element: <ChatView /> },
      { path: "history",      element: <HistoryView /> },
      { path: "settings",     element: <SettingsPanel /> },
      { path: "profile",      element: <ProfilePage /> },
      { path: "*",            element: <Navigate to="/" replace /> },
    ],
  },
];
