// matchblox.sh is the install command: `curl -fsSL https://matchblox.sh | sh`.
// A command-line client on matchblox.sh gets the install script; a browser
// goes to matchblox.com, where the script is one link away. Every other host
// is the static site in www/. Under /push/ matchblox.sh is the push relay
// (push.js), for curl and browsers alike.

import { handlePush } from "./push.js";

const CLI = /^(curl|wget|fetch|httpie|powershell|libfetch|aria2)\b/i;

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.hostname !== "matchblox.sh" && !url.hostname.endsWith(".matchblox.sh")) {
      return env.ASSETS.fetch(request);
    }
    if (url.pathname.startsWith("/push/")) {
      return handlePush(request, env);
    }
    const cli = CLI.test(request.headers.get("user-agent") || "");
    if (url.pathname === "/install.sh" || (url.pathname === "/" && cli)) {
      const script = await env.ASSETS.fetch(new Request(new URL("/install.sh", url), request));
      const res = new Response(script.body, script);
      res.headers.set("Content-Type", "text/plain; charset=utf-8");
      res.headers.set("Cache-Control", "public, max-age=300");
      return res;
    }
    return Response.redirect("https://matchblox.com" + url.pathname + url.search, 302);
  },
};
