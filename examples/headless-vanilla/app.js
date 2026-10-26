(function () {
  const out = document.getElementById("out");
  const baseInput = document.getElementById("base");

  function base() {
    return String(baseInput.value || "").replace(/\/$/, "");
  }

  let sessionToken = "";

  function token() {
    return sessionToken;
  }

  function headers(extra) {
    const h = Object.assign({ Accept: "application/json" }, extra || {});
    const t = token();
    if (t) h.Authorization = "Bearer " + t;
    return h;
  }

  async function api(path, options) {
    const res = await fetch(base() + path, options);
    const text = await res.text();
    let body = text;
    try {
      body = text ? JSON.parse(text) : null;
    } catch (err) {
      /* keep raw text */
    }
    if (!res.ok) {
      throw new Error(res.status + " " + (body && body.error && body.error.message ? body.error.message : text));
    }
    return body && body.data !== undefined ? body.data : body;
  }

  function show(value) {
    out.textContent = typeof value === "string" ? value : JSON.stringify(value, null, 2);
  }

  document.getElementById("load-product").addEventListener("click", async function () {
    try {
      const key = document.getElementById("slug").value.trim();
      show(await api("/api/v1/products/" + encodeURIComponent(key)));
    } catch (err) {
      show(String(err.message || err));
    }
  });

  document.getElementById("login").addEventListener("click", async function () {
    try {
      const data = await api("/api/v1/auth/login", {
        method: "POST",
        headers: headers({ "Content-Type": "application/json" }),
        body: JSON.stringify({
          email: document.getElementById("email").value,
          password: document.getElementById("password").value
        })
      });
      if (data && data.token) {
        sessionToken = data.token;
      }
      show(data);
    } catch (err) {
      show(String(err.message || err));
    }
  });

  document.getElementById("addresses").addEventListener("click", async function () {
    try {
      show(await api("/api/v1/account/addresses", { headers: headers() }));
    } catch (err) {
      show(String(err.message || err));
    }
  });

  document.getElementById("add-cart").addEventListener("click", async function () {
    try {
      const key = document.getElementById("slug").value.trim();
      const variants = await api("/api/v1/products/" + encodeURIComponent(key) + "/variants");
      const first = (variants.variants || variants || [])[0];
      const variantId = first && (first.id || first.ID);
      if (!variantId) {
        throw new Error("no variant on this product");
      }
      const cart = await api("/api/v1/carts", {
        method: "POST",
        headers: headers({ "Content-Type": "application/json" }),
        body: JSON.stringify({ currency: "EUR" })
      });
      const cartId = cart.id || cart.cart_id || (cart.cart && cart.cart.id);
      const updated = await api("/api/v1/carts/" + encodeURIComponent(cartId) + "/items", {
        method: "POST",
        headers: headers({ "Content-Type": "application/json" }),
        body: JSON.stringify({ variant_id: variantId, quantity: 1 })
      });
      localStorage.setItem("shopanda_cart_id", cartId);
      show(updated);
    } catch (err) {
      show(String(err.message || err));
    }
  });
})();
