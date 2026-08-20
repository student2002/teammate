// use-permission.ts 提供前端权限判断 Hook。
import { useAppStore } from "@/lib/store";

const roleLevels: Record<string, number> = {
  owner: 4,
  admin: 3,
  member: 2,
  viewer: 1,
};

/**
 * Hook that provides role-based permission checks for the current user.
 * Use this to conditionally render UI elements based on the user's workspace role.
 */
export function usePermission() {
  const user = useAppStore((s) => s.user);

  const role = user?.role ?? "";
  const level = roleLevels[role] ?? 0;

  return {
    /** Current user's role string */
    role,
    /** Numeric role level (owner=4, admin=3, member=2, viewer=1, unknown=0) */
    level,
    /** Whether the user is the workspace owner */
    isOwner: role === "owner",
    /** Whether the user is an admin or owner */
    isAdmin: role === "admin" || role === "owner",
    /** Whether the user can manage members (invite, remove, change roles) */
    canManageMembers: role === "owner" || role === "admin",
    /** Whether the user can create/edit tasks, agents, workflows, etc. */
    canEdit: role === "owner" || role === "admin" || role === "member",
    /** Whether the user is a viewer (read-only) */
    isViewer: role === "viewer",
    /** Whether the user is authenticated */
    isAuthenticated: !!user,
  };
}
