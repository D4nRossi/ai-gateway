import { Navigate, useLocation } from "react-router-dom";
import { useSession } from "@/lib/useAuth";

interface Props {
  children: React.ReactNode;
}

/**
 * AuthGuard — redirects unauthenticated users to /login while preserving the
 * intended destination so we can bounce them back after a successful sign-in.
 */
export function AuthGuard({ children }: Props) {
  const session = useSession();
  const location = useLocation();

  if (!session) {
    return <Navigate to="/login" replace state={{ from: location }} />;
  }
  return <>{children}</>;
}
