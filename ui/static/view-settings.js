// "Ajustes": the user's choices.

import { ApiError, SessionExpired, api } from "./api.js";
import { h } from "./dom.js";

const CONFIRM_WORD = "APAGAR";

const messageOf = (error) => (error instanceof ApiError ? error.message : "Não foi possível falar com o AvaNet.");

function profileCard(profile, current) {
  return h("label", { class: "profile" },
    h("input", { type: "radio", name: "profile", value: profile.id, checked: profile.id === current }),
    h("strong", null, profile.name),
    h("span", null, profile.description),
    h("small", null, `Mede a cada ${profile.light_interval} · velocidade a cada ${profile.speed_interval}`));
}

function numberField(label, name, value, { min, max, step = 1, hint }) {
  return h("label", { class: "field" },
    h("span", null, label),
    h("input", { type: "number", name, value, min, max, step, required: true, inputmode: "numeric" }),
    hint && h("small", null, hint));
}

function settingsForm(data, onError) {
  const { preferences: p, limits } = data;
  const status = h("span", { class: "status", role: "status" });
  const say = (text, isError) => {
    status.textContent = text;
    status.classList.toggle("error", Boolean(isError));
  };

  const form = h("form", {
    class: "settings", novalidate: false,
    onsubmit: async (event) => {
      event.preventDefault();
      const values = new FormData(form);
      say("Salvando…");
      try {
        await api.saveSettings({
          profile: values.get("profile"),
          fixed_daily_mb: Number(values.get("fixed_daily_mb")),
          mobile_enabled: values.get("mobile_enabled") === "on",
          mobile_daily_mb: Number(values.get("mobile_daily_mb")),
          plan_download_mbps: Number(values.get("plan_download_mbps")),
          plan_upload_mbps: Number(values.get("plan_upload_mbps")),
        });
        say("Salvo. Já está valendo.");
      } catch (error) {
        if (error instanceof SessionExpired) onError(error);
        else say(messageOf(error), true);
      }
    },
  },
  h("fieldset", null,
    h("legend", null, "Para que você mais usa a internet?"),
    h("p", { class: "help" }, "O perfil define o que conta como bom ou ruim nos boletins e com que frequência o AvaNet mede. Trocar de perfil também recalcula os boletins antigos."),
    h("div", { class: "profiles" }, data.profiles.map((profile) => profileCard(profile, p.profile)))),

  h("fieldset", null,
    h("legend", null, "Quanto de dados o AvaNet pode usar"),
    h("p", { class: "help" }, "Só os testes de velocidade gastam dados de verdade. Ao chegar no limite, os testes param até a meia-noite; o resto do monitoramento continua."),
    h("div", { class: "fields" },
      numberField("Internet de casa, por dia (MB)", "fixed_daily_mb", p.fixed_daily_mb, { min: limits.min_fixed_daily_mb, max: limits.max_fixed_daily_mb, hint: `Entre ${limits.min_fixed_daily_mb} e ${limits.max_fixed_daily_mb} MB. Um teste costuma gastar até 145 MB.` }),
      numberField("Dados móveis, por dia (MB)", "mobile_daily_mb", p.mobile_daily_mb, { min: limits.min_mobile_daily_mb, max: limits.max_mobile_daily_mb, hint: `Entre ${limits.min_mobile_daily_mb} e ${limits.max_mobile_daily_mb} MB. Só vale com a opção abaixo ligada.` })),
    h("label", { class: "check spaced" },
      h("input", { type: "checkbox", name: "mobile_enabled", checked: p.mobile_enabled }),
      h("span", null, "Fazer testes de velocidade em dados móveis", h("small", null, "Hotspot do celular ou modem. Desligado para não consumir sua franquia sem você querer."))),
  ),

  h("fieldset", null,
    h("legend", null, "Seu plano contratado"),
    h("p", { class: "help" }, "Opcional. A velocidade do plano aparece como referência no gráfico do boletim. Use 0 se não souber."),
    h("div", { class: "fields" },
      numberField("Download (Mbps)", "plan_download_mbps", p.plan_download_mbps, { min: 0, max: limits.max_plan_mbps, step: "any" }),
      numberField("Upload (Mbps)", "plan_upload_mbps", p.plan_upload_mbps, { min: 0, max: limits.max_plan_mbps, step: "any" }))),

  h("div", { class: "form-actions" }, h("button", { class: "button", type: "submit" }, "Salvar ajustes"), status));
  return form;
}

function autostartSection(autostart, onError) {
  const status = h("span", { class: "status", role: "status" });
  const box = h("input", {
    type: "checkbox", checked: autostart.enabled, disabled: !autostart.available,
    onchange: async () => {
      const wanted = box.checked;
      status.textContent = "";
      status.classList.remove("error");
      box.disabled = true;
      try {
        const result = await api.setAutostart(wanted);
        box.checked = result.enabled;
        status.textContent = result.enabled ? "O AvaNet vai iniciar com o Windows." : "O AvaNet não vai mais iniciar com o Windows.";
      } catch (error) {
        if (error instanceof SessionExpired) { onError(error); return; }
        box.checked = !wanted;
        status.textContent = messageOf(error);
        status.classList.add("error");
      } finally {
        box.disabled = false;
      }
    },
  });
  return h("fieldset", null,
    h("legend", null, "Iniciar com o Windows"),
    h("label", { class: "check" }, box, h("span", null, "Abrir o AvaNet quando o computador ligar",
      h("small", null, autostart.available ? "Sem isso, nada é medido até você abrir o programa." : "Não disponível neste sistema."))),
    status);
}

function historySection(onError) {
  const input = h("input", { type: "text", autocomplete: "off", "aria-label": `Digite ${CONFIRM_WORD} para confirmar` });
  const confirmButton = h("button", { class: "button danger", type: "button", disabled: true }, "Apagar tudo");
  const feedback = h("span", { class: "status", role: "status" });
  const close = () => { dialog.close(); input.value = ""; confirmButton.disabled = true; };

  input.addEventListener("input", () => { confirmButton.disabled = input.value !== CONFIRM_WORD; });
  confirmButton.addEventListener("click", async () => {
    confirmButton.disabled = true;
    try {
      await api.deleteHistory(input.value);
      close();
      feedback.classList.remove("error");
      feedback.textContent = "Histórico apagado.";
    } catch (error) {
      close();
      if (error instanceof SessionExpired) { onError(error); return; }
      feedback.textContent = messageOf(error);
      feedback.classList.add("error");
    }
  });

  const dialog = h("dialog", null,
    h("h2", null, "Apagar todo o histórico?"),
    h("p", null, "Medições, testes de velocidade, quedas, boletins e nomes de redes serão apagados. Não dá para desfazer."),
    h("p", null, `Digite ${CONFIRM_WORD} para confirmar.`),
    h("div", { class: "field" }, input),
    h("div", { class: "row" },
      h("button", { class: "button quiet", type: "button", onclick: close }, "Cancelar"),
      confirmButton));

  return h("fieldset", null,
    h("legend", null, "Apagar histórico"),
    h("p", { class: "help" }, "Tudo fica só neste computador. Se quiser recomeçar, dá para apagar o que foi medido. Seus ajustes e o consumo de dados de hoje são mantidos."),
    h("div", { class: "inline-actions" },
      h("button", { class: "button quiet", type: "button", onclick: () => dialog.showModal() }, "Apagar histórico…"),
      feedback),
    dialog);
}

export async function settingsView(container, onError) {
  try {
    const data = await api.settings();
    container.replaceChildren(
      settingsForm(data, onError),
      h("div", { class: "settings" }, autostartSection(data.autostart, onError), historySection(onError)),
    );
  } catch (error) {
    onError(error);
  }
}
