const state = { toastTimer: null };

document.addEventListener('DOMContentLoaded', () => {
  setupTabs();
  bindForms();
  refreshStatus();
  refreshLogs();
  setInterval(refreshStatus, 3000);
  setInterval(refreshLogs, 3000);
});

// As abas representam apenas estado local da UI; o servidor sempre renderiza a página completa.
function setupTabs() {
  document.querySelectorAll('.tab-button').forEach(button => {
    button.addEventListener('click', () => {
      document.querySelectorAll('.tab-button').forEach(btn => btn.classList.remove('active'));
      document.querySelectorAll('.tab-content').forEach(content => content.classList.remove('active'));
      button.classList.add('active');
      document.getElementById(button.dataset.tab).classList.add('active');
    });
  });
}

// bindForms centraliza as ações do usuário para facilitar leitura e manutenção do fluxo.
function bindForms() {
  document.getElementById('connectForm').addEventListener('submit', async (event) => {
    event.preventDefault();
    const payload = formToJSON(event.target);
    payload.heartbeatIntervalSec = parseInt(payload.heartbeatIntervalSec || '30', 10);
    payload.tokenRefreshMinutes = parseInt(payload.tokenRefreshMinutes || '25', 10);
    payload.insecureTls = Boolean(payload.insecureTls);

    const res = await api('/api/connect', 'POST', payload);
    if (res.ok) {
      showToast('Conexão estabelecida com sucesso.', 'success');
      refreshStatus();
      refreshLogs();
    } else {
      showToast(res.error || 'Falha ao conectar.', 'error');
    }
  });

  document.getElementById('disconnectBtn').addEventListener('click', async () => {
    const res = await api('/api/disconnect', 'POST', {});
    if (res.ok) {
      showToast('Sessão encerrada.', 'success');
      refreshStatus();
      refreshLogs();
    } else {
      showToast(res.error || 'Falha ao desconectar.', 'error');
    }
  });

  document.getElementById('bridgeConfigBtn').addEventListener('click', async () => {
    const res = await api('/api/bridge-config', 'GET');
    const pre = document.getElementById('bridgeConfigResult');
    if (res.ok) {
      pre.textContent = pretty(res.data);
      showToast('Configuração consultada com sucesso.', 'success');
    } else {
      pre.textContent = res.error || 'Erro ao consultar configuração.';
      showToast(res.error || 'Erro ao consultar configuração.', 'error');
    }
  });

  document.getElementById('eventForm').addEventListener('submit', async (event) => {
    event.preventDefault();
    const payload = formToJSON(event.target);
    if (payload.eventTimeUnix) {
      payload.eventTimeUnix = parseInt(payload.eventTimeUnix, 10);
    } else {
      delete payload.eventTimeUnix;
    }

    const res = await api('/api/send-event', 'POST', payload);
    const pre = document.getElementById('eventResponse');
    if (res.ok) {
      pre.textContent = pretty(res.data);
      showToast('Evento enviado com sucesso.', 'success');
      refreshLogs();
    } else {
      pre.textContent = res.error || 'Erro ao enviar evento.';
      showToast(res.error || 'Erro ao enviar evento.', 'error');
    }
  });
}

// refreshStatus atualiza o resumo da sessão sem interferir no estado dos formulários.
async function refreshStatus() {
  const res = await api('/api/status', 'GET');
  if (!res.ok) return;

  const status = res.data || {};
  const badge = document.getElementById('statusBadge');
  badge.textContent = status.connected ? 'Conectado' : 'Desconectado';
  badge.classList.toggle('online', Boolean(status.connected));
  badge.classList.toggle('offline', !status.connected);

  document.getElementById('statusBaseUrl').textContent = status.baseUrl || '—';
  document.getElementById('statusBridgeId').textContent = status.bridgeId || '—';
  document.getElementById('statusAuthorize').textContent = formatDate(status.lastAuthorizeAt);
  document.getElementById('statusHeartbeat').textContent = formatDate(status.lastHeartbeatAt);
  document.getElementById('statusTokenExpiry').textContent = formatDate(status.tokenExpiresAt);

  const errCard = document.getElementById('lastErrorCard');
  if (status.lastError) {
    errCard.textContent = status.lastError;
    errCard.classList.remove('hidden');
  } else {
    errCard.classList.add('hidden');
    errCard.textContent = '';
  }
}

// refreshLogs redesenha o buffer de logs do servidor em ordem cronológica reversa.
async function refreshLogs() {
  const res = await api('/api/logs', 'GET');
  if (!res.ok) return;

  const container = document.getElementById('logs');
  const logs = Array.isArray(res.data) ? res.data : [];
  if (!logs.length) {
    container.innerHTML = '<div class="log-entry"><div class="log-meta"><span>INFO</span><span>—</span></div><div>Aguardando ações...</div></div>';
    return;
  }

  container.innerHTML = logs.slice().reverse().map(log => `
    <div class="log-entry">
      <div class="log-meta">
        <span class="log-level ${escapeHtml(log.level || 'INFO')}">${escapeHtml(log.level || 'INFO')}</span>
        <span>${escapeHtml(formatDate(log.time))}</span>
      </div>
      <div>${escapeHtml(log.message || '')}</div>
    </div>
  `).join('');
}

// formToJSON mantém apenas o primeiro valor por campo, já que esta UI não usa arrays.
function formToJSON(form) {
  const formData = new FormData(form);
  const data = {};
  for (const [key, value] of formData.entries()) {
    if (data[key] !== undefined) continue;
    data[key] = value;
  }
  form.querySelectorAll('input[type="checkbox"]').forEach(input => {
    data[input.name] = input.checked;
  });
  return data;
}

// api padroniza requisições/respostas JSON e normaliza erros de transporte para a UI.
async function api(url, method = 'GET', body) {
  try {
    const options = { method, headers: { 'Accept': 'application/json' } };
    if (body !== undefined && method !== 'GET') {
      options.headers['Content-Type'] = 'application/json';
      options.body = JSON.stringify(body);
    }

    const response = await fetch(url, options);
    const text = await response.text();
    let json = null;
    try { json = text ? JSON.parse(text) : null; } catch (_) {}

    if (!response.ok) {
      return { ok: false, error: json?.error || text || `HTTP ${response.status}` };
    }
    return { ok: true, data: json };
  } catch (error) {
    return { ok: false, error: error.message || 'Erro inesperado' };
  }
}

function pretty(value) { return JSON.stringify(value, null, 2); }

// formatDate evita que timestamps vazios ou inválidos vazem para a UI.
function formatDate(value) {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleString('pt-BR');
}

function showToast(message, type) {
  const toast = document.getElementById('toast');
  toast.textContent = message;
  toast.className = `toast ${type}`;
  clearTimeout(state.toastTimer);
  state.toastTimer = setTimeout(() => {
    toast.className = 'toast hidden';
    toast.textContent = '';
  }, 3400);
}

// escapeHtml é necessário porque os logs são renderizados com innerHTML.
function escapeHtml(value) {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
}
