import { Outlet, Scripts } from "react-router";

export default function App() {
  return (
    <html>
      <body>
        <Outlet />
        <Scripts />
      </body>
    </html>
  );
}
