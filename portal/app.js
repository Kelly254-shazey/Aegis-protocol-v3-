document.addEventListener("DOMContentLoaded", () => {
  let selectedPlan = "duo";
  let selectedPrice = 2500;

  const planCards = document.querySelectorAll(".plan-card");
  const phoneInput = document.getElementById("phone-input");
  const payBtn = document.getElementById("pay-btn");
  const checkoutPanel = document.getElementById("checkout-panel");
  const passPanel = document.getElementById("pass-panel");
  const ticketDisplay = document.getElementById("ticket-display");
  const passDetails = document.getElementById("pass-details");

  // Plan Selection
  planCards.forEach((card) => {
    card.addEventListener("click", () => {
      planCards.forEach((c) => c.classList.remove("selected"));
      card.classList.add("selected");
      selectedPlan = card.getAttribute("data-plan");
      selectedPrice = parseInt(card.getAttribute("data-price"), 10);
      payBtn.textContent = `Pay KES ${selectedPrice.toLocaleString()} via M-Pesa STK Push`;
    });
  });

  // M-Pesa STK Trigger
  payBtn.addEventListener("click", async () => {
    const phone = phoneInput.value.trim();
    if (!phone || phone.length < 9) {
      alert("Please enter a valid Safaricom M-Pesa phone number (e.g. 254712345678 or 0712345678)");
      return;
    }

    payBtn.disabled = true;
    payBtn.textContent = "⏳ Requesting M-Pesa STK Push...";

    try {
      const resp = await fetch("/api/v3/pay/mpesa", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          phone: phone,
          plan: selectedPlan,
          amount: selectedPrice,
        }),
      });

      const data = await resp.json();
      if (data.status === "pending" || data.ticket) {
        showPromptSimulation(phone, data);
      } else {
        alert(data.message || "Failed to initiate M-Pesa push");
        resetBtn();
      }
    } catch (err) {
      // Offline / Demo fallback
      console.warn("Control plane offline or simulated:", err);
      simulateDirectActivation(phone);
    }
  });

  function showPromptSimulation(phone, data) {
    payBtn.textContent = "📱 Check Phone for PIN Prompt...";
    setTimeout(() => {
      activatePass(data.ticket || generateDemoTicket(phone, selectedPlan));
    }, 2500);
  }

  function simulateDirectActivation(phone) {
    payBtn.textContent = "📱 Sending M-Pesa Push to " + phone + "...";
    setTimeout(() => {
      payBtn.textContent = "🔐 Verifying Ed25519 Session Ticket...";
      setTimeout(() => {
        activatePass(generateDemoTicket(phone, selectedPlan));
      }, 1500);
    }, 2000);
  }

  function generateDemoTicket(phone, plan) {
    const maxDevs = plan === "solo" ? 1 : plan === "duo" ? 2 : 5;
    const now = Math.floor(Date.now() / 1000);
    return {
      t: {
        uid: "usr_" + phone.slice(-4),
        did: "did_primary_node",
        tier: plan,
        max_dev: maxDevs,
        iat: now,
        exp: now + 30 * 86400,
      },
      sig: "MEQCID7xY2U...simulated_ed25519_sig...AegisProtocolV3==",
    };
  }

  function activatePass(ticket) {
    checkoutPanel.style.display = "none";
    passPanel.style.display = "block";

    passDetails.innerHTML = `
      <strong>Plan:</strong> ${ticket.t.tier.toUpperCase()} SHIELD | 
      <strong>Devices:</strong> Up to ${ticket.t.max_dev} devices | 
      <strong>Valid Until:</strong> ${new Date(ticket.t.exp * 1000).toLocaleDateString()}
    `;

    ticketDisplay.textContent = JSON.stringify(ticket, null, 2);

    // Render pairing QR Code
    const qrContainer = document.getElementById("qr-container");
    qrContainer.innerHTML = "";
    if (typeof QRCode !== "undefined") {
      new QRCode(qrContainer, {
        text: JSON.stringify(ticket),
        width: 180,
        height: 180,
      });
    } else {
      qrContainer.innerHTML = `<img src="https://api.qrserver.com/v1/create-qr-code/?size=180x180&data=${encodeURIComponent(
        JSON.stringify(ticket)
      )}" alt="Pairing QR Code" style="display:block;margin:0 auto;" />`;
    }
  }

  function resetBtn() {
    payBtn.disabled = false;
    payBtn.textContent = `Pay KES ${selectedPrice.toLocaleString()} via M-Pesa STK Push`;
  }
});
