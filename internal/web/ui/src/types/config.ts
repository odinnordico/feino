// Typed wrappers and helpers over generated ConfigProto.
// The proto types are the ground truth; these add convenience.

export type PermissionLevel = "read" | "write" | "bash" | "danger_zone";
export type Theme = "dark" | "light" | "auto" | "neo";
export type CommStyle = "concise" | "detailed" | "technical" | "friendly";

export type ProviderInfo = {
  name: string;
  label: string;
  hasKey: boolean;
  defaultModel: string;
}
