// Passkey buttons on Fling Gateway pages: [data-passkey-login] signs in, [data-passkey-register]
// adds a passkey (data-link carries an invite or reset link). Errors go to [data-passkey-error].
(() => {
  const toBuf = (s) => {
    s = s.replace(/-/g, "+").replace(/_/g, "/");
    s += "=".repeat((4 - (s.length % 4)) % 4);
    return Uint8Array.from(atob(s), (c) => c.charCodeAt(0)).buffer;
  };
  const toB64 = (b) =>
    btoa(String.fromCharCode(...new Uint8Array(b))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");

  const creationOptions = (o) => {
    if (PublicKeyCredential.parseCreationOptionsFromJSON) return PublicKeyCredential.parseCreationOptionsFromJSON(o.publicKey);
    const pk = o.publicKey;
    pk.challenge = toBuf(pk.challenge);
    pk.user.id = toBuf(pk.user.id);
    (pk.excludeCredentials || []).forEach((c) => (c.id = toBuf(c.id)));
    return pk;
  };
  const requestOptions = (o) => {
    if (PublicKeyCredential.parseRequestOptionsFromJSON) return PublicKeyCredential.parseRequestOptionsFromJSON(o.publicKey);
    const pk = o.publicKey;
    pk.challenge = toBuf(pk.challenge);
    (pk.allowCredentials || []).forEach((c) => (c.id = toBuf(c.id)));
    return pk;
  };
  const credentialJSON = (c) => {
    try {
      if (c.toJSON) return c.toJSON();
    } catch (e) {}
    const r = c.response;
    const response = { clientDataJSON: toB64(r.clientDataJSON) };
    if (r.attestationObject) {
      response.attestationObject = toB64(r.attestationObject);
      response.transports = r.getTransports ? r.getTransports() : [];
    } else {
      response.authenticatorData = toB64(r.authenticatorData);
      response.signature = toB64(r.signature);
      if (r.userHandle) response.userHandle = toB64(r.userHandle);
    }
    return { id: c.id, rawId: toB64(c.rawId), type: c.type, response, clientExtensionResults: c.getClientExtensionResults() };
  };

  const post = async (url, body) => {
    const res = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify(body),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || "Something went wrong. Try again.");
    return data;
  };

  const showError = (button, e) => {
    const box = button.parentElement.querySelector("[data-passkey-error]");
    if (!box) return;
    box.textContent =
      e && e.name === "NotAllowedError"
        ? "The passkey prompt was closed or timed out. Try again."
        : e && e.name === "InvalidStateError"
          ? "This device already has a passkey for this account."
          : (e && e.message) || "Something went wrong. Try again.";
    box.hidden = false;
  };

  const run = (button, task) =>
    button.addEventListener("click", async () => {
      button.disabled = true;
      try {
        location.assign((await task()).redirect);
      } catch (e) {
        showError(button, e);
        button.disabled = false;
      }
    });

  document.querySelectorAll("[data-passkey-login], [data-passkey-register]").forEach((button) => {
    if (!window.PublicKeyCredential) {
      button.hidden = true;
      return;
    }
    if (button.hasAttribute("data-passkey-login")) {
      run(button, async () => {
        const options = await post("/auth/passkey/login/begin", { flow: button.dataset.flow || "" });
        const cred = await navigator.credentials.get({ publicKey: requestOptions(options) });
        return post("/auth/passkey/login/finish", credentialJSON(cred));
      });
    } else {
      run(button, async () => {
        const options = await post("/auth/passkey/register/begin", { link: button.dataset.link || "" });
        const cred = await navigator.credentials.create({ publicKey: creationOptions(options) });
        return post("/auth/passkey/register/finish", credentialJSON(cred));
      });
    }
  });
})();
