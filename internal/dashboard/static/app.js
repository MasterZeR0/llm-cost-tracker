let charts = {};

async function fetchJSON(url) {
  const res = await fetch(url);
  return res.json();
}

function formatTokens(num) {
  if (num === null || num === undefined) return '0';
  if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
  if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
  return num.toLocaleString();
}

function formatDuration(sec) {
  if (!sec || sec < 0) return '0s';
  if (sec < 60) return Math.round(sec) + 's';
  const m = Math.floor(sec / 60);
  const s = Math.round(sec % 60);
  if (m < 60) return `${m}m ${s}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${m % 60}m`;
}

async function loadDashboard() {
  const period = document.getElementById('period').value;

  const overview = await fetchJSON(`/api/overview?period=${encodeURIComponent(period)}`);
  document.getElementById('total-requests').innerText = (overview.total_requests || 0).toLocaleString();
  document.getElementById('total-tokens').innerText = formatTokens(overview.total_tokens || 0);
  document.getElementById('total-cost').innerText = '$' + (overview.total_cost || 0).toFixed(2);
  document.getElementById('avg-latency').innerText = ((overview.avg_latency_ms || 0) / 1000).toFixed(2) + 's';

  // Cache efficiency & total sessions stat cards
  const hitRate = overview.cache_hit_rate || 0;
  const savings = overview.cache_savings_usd || 0;
  document.getElementById('cache-hit-rate').innerText = hitRate.toFixed(1) + '%';
  document.getElementById('cache-savings').innerText = '$' + savings.toFixed(2) + ' saved';

  const totalSessions = overview.total_sessions || 0;
  const turnsPerSess = overview.avg_turns_per_session || 0;
  document.getElementById('total-sessions').innerText = totalSessions.toLocaleString();
  document.getElementById('turns-per-session').innerText = turnsPerSess.toFixed(1) + ' turns/sess';

  loadCostOverTime(period);
  loadCostByModel(period);
  loadTokensByModel(period);
  loadToolUsage(period);
  loadToolInvocations(period);
  loadTokenComposition(period);
  loadSessions(period);
  loadRequests(period);
}

async function loadCostOverTime(period) {
  const data = await fetchJSON(`/api/cost-over-time?period=${encodeURIComponent(period)}`);
  const labels = [...new Set(data.map(d => d.date))];
  const costs = labels.map(l => {
    return data.filter(d => d.date === l).reduce((acc, cur) => acc + cur.cost, 0);
  });

  if (charts.costTime) charts.costTime.destroy();
  charts.costTime = new Chart(document.getElementById('costTimeChart'), {
    type: 'line',
    data: {
      labels: labels,
      datasets: [{
        label: 'Cost ($)',
        data: costs,
        borderColor: '#58a6ff',
        tension: 0.2,
        fill: true,
        backgroundColor: 'rgba(88, 166, 255, 0.1)'
      }]
    },
    options: { responsive: true }
  });
}

async function loadCostByModel(period) {
  const data = await fetchJSON(`/api/cost-by-model?period=${encodeURIComponent(period)}`);
  if (charts.costModel) charts.costModel.destroy();
  charts.costModel = new Chart(document.getElementById('costModelChart'), {
    type: 'bar',
    data: {
      labels: data.map(d => d.model),
      datasets: [{
        label: 'Cost ($)',
        data: data.map(d => d.cost),
        backgroundColor: '#3fb950'
      }]
    },
    options: { indexAxis: 'y', responsive: true }
  });
}

async function loadTokensByModel(period) {
  const data = await fetchJSON(`/api/tokens-by-model?period=${encodeURIComponent(period)}`);
  if (charts.tokensModel) charts.tokensModel.destroy();
  charts.tokensModel = new Chart(document.getElementById('tokensModelChart'), {
    type: 'bar',
    data: {
      labels: data.map(d => d.model),
      datasets: [
        { label: 'Input Tokens', data: data.map(d => d.input_tokens), backgroundColor: '#1f6feb' },
        { label: 'Output Tokens', data: data.map(d => d.output_tokens), backgroundColor: '#a371f7' }
      ]
    },
    options: { responsive: true, scales: { x: { stacked: true }, y: { stacked: true } } }
  });
}

async function loadToolUsage(period) {
  const data = await fetchJSON(`/api/tool-usage?period=${encodeURIComponent(period)}`);
  if (charts.toolUsage) charts.toolUsage.destroy();
  charts.toolUsage = new Chart(document.getElementById('toolUsageChart'), {
    type: 'doughnut',
    data: {
      labels: data.map(d => d.tool),
      datasets: [{
        data: data.map(d => d.request_count),
        backgroundColor: ['#58a6ff', '#3fb950', '#d29922', '#db61a2', '#a371f7']
      }]
    },
    options: { responsive: true }
  });
}

async function loadToolInvocations(period) {
  const data = await fetchJSON(`/api/analytics/tools?period=${encodeURIComponent(period)}`);
  if (charts.toolInvocations) charts.toolInvocations.destroy();

  const labels = data && data.length > 0 ? data.map(d => d.tool_name) : ['None'];
  const values = data && data.length > 0 ? data.map(d => d.total_calls) : [0];
  const colors = ['#58a6ff', '#3fb950', '#d29922', '#db61a2', '#a371f7', '#f85149', '#79c0ff', '#56d364', '#e3b341', '#f0883e'];

  charts.toolInvocations = new Chart(document.getElementById('toolInvocationsChart'), {
    type: 'doughnut',
    data: {
      labels: labels,
      datasets: [{
        data: values,
        backgroundColor: colors.slice(0, labels.length)
      }]
    },
    options: { responsive: true }
  });
}

async function loadTokenComposition(period) {
  const data = await fetchJSON(`/api/analytics/cache?period=${encodeURIComponent(period)}`);
  if (charts.tokenComposition) charts.tokenComposition.destroy();

  const miss = data.cache_miss_tokens || 0;
  const hit = data.cache_hit_tokens || 0;
  const output = data.output_tokens || 0;

  charts.tokenComposition = new Chart(document.getElementById('tokenCompositionChart'), {
    type: 'doughnut',
    data: {
      labels: ['Prompt Miss', 'Cache Hit', 'Output'],
      datasets: [{
        data: [miss, hit, output],
        backgroundColor: ['#d29922', '#3fb950', '#a371f7']
      }]
    },
    options: { responsive: true }
  });
}

async function loadSessions(period) {
  const url = `/api/sessions?period=${encodeURIComponent(period)}&limit=50`;
  const sessions = await fetchJSON(url);
  const tbody = document.getElementById('sessions-table');
  tbody.innerHTML = '';

  if (!sessions || sessions.length === 0) {
    const tr = document.createElement('tr');
    tr.innerHTML = '<td colspan="8" style="text-align: center; color: #8b949e;">No sessions found</td>';
    tbody.appendChild(tr);
    return;
  }

  sessions.forEach(s => {
    const tr = document.createElement('tr');
    const sidDisplay = s.session_id.length > 14 ? s.session_id.substring(0, 12) + '…' : s.session_id;
    const duration = formatDuration(s.duration_seconds);
    const cacheRate = (s.cache_hit_rate || 0).toFixed(1) + '%';
    const cost = '$' + (s.total_cost || 0).toFixed(4);
    let tools = `${s.tool_call_count || 0}`;
    if (s.tool_error_count > 0) {
      tools += ` <span style="color: #f85149">(${s.tool_error_count} err)</span>`;
    }

    tr.innerHTML = `
      <td><code title="${s.session_id}">${sidDisplay}</code></td>
      <td>${s.tool || '—'}</td>
      <td>${s.turn_count || 0}</td>
      <td>${duration}</td>
      <td>${cacheRate}</td>
      <td>${cost}</td>
      <td>${tools}</td>
      <td><button class="btn btn-sm" onclick="showSessionDetail('${encodeURIComponent(s.session_id)}')">Drilldown</button></td>
    `;
    tbody.appendChild(tr);
  });
}

async function loadRequests(period) {
  const url = period ? `/api/requests?period=${encodeURIComponent(period)}` : '/api/requests';
  const reqs = await fetchJSON(url);
  const tbody = document.getElementById('requests-table');
  tbody.innerHTML = '';
  reqs.forEach(r => {
    const tr = document.createElement('tr');
    const cost = r.cost_usd ? '$' + r.cost_usd.toFixed(4) : '—';
    const latency = r.latency_ms ? (r.latency_ms / 1000).toFixed(2) + 's' : '—';
    tr.innerHTML = `
      <td>${new Date(r.timestamp).toLocaleTimeString()}</td>
      <td>${r.tool}</td>
      <td>${r.model || '—'}</td>
      <td>${(r.input_tokens || 0).toLocaleString()}</td>
      <td>${(r.output_tokens || 0).toLocaleString()}</td>
      <td>${cost}</td>
      <td>${latency}</td>
    `;
    tbody.appendChild(tr);
  });
}

async function showSessionDetail(rawSessionId) {
  const sessionId = decodeURIComponent(rawSessionId);
  const modal = document.getElementById('session-modal');
  modal.style.display = 'flex';

  document.getElementById('modal-title').innerText = `Session: ${sessionId}`;
  const summaryEl = document.getElementById('modal-summary');
  summaryEl.innerHTML = '<div style="color: #8b949e">Loading session details...</div>';
  const turnsEl = document.getElementById('modal-turns-table');
  turnsEl.innerHTML = '';

  try {
    const detail = await fetchJSON(`/api/sessions/detail?id=${encodeURIComponent(sessionId)}`);
    const sm = detail.summary;

    summaryEl.innerHTML = `
      <div class="summary-item">
        <span>Tool</span>
        <strong>${sm.tool || '—'}</strong>
      </div>
      <div class="summary-item">
        <span>Turns</span>
        <strong>${sm.turn_count || 0}</strong>
      </div>
      <div class="summary-item">
        <span>Duration</span>
        <strong>${formatDuration(sm.duration_seconds)}</strong>
      </div>
      <div class="summary-item">
        <span>Total Tokens</span>
        <strong>${formatTokens(sm.total_tokens || 0)}</strong>
      </div>
      <div class="summary-item">
        <span>Cache Hit Rate</span>
        <strong>${(sm.cache_hit_rate || 0).toFixed(1)}%</strong>
      </div>
      <div class="summary-item">
        <span>Total Cost</span>
        <strong>$${(sm.total_cost || 0).toFixed(4)}</strong>
      </div>
      <div class="summary-item">
        <span>Tool Calls</span>
        <strong>${sm.tool_call_count || 0}${sm.tool_error_count > 0 ? ` (${sm.tool_error_count} err)` : ''}</strong>
      </div>
    `;

    turnsEl.innerHTML = '';
    if (!detail.turns || detail.turns.length === 0) {
      turnsEl.innerHTML = '<tr><td colspan="9" style="text-align: center; color: #8b949e">No turns recorded</td></tr>';
      return;
    }

    detail.turns.forEach((t, idx) => {
      const tr = document.createElement('tr');
      const stepNum = t.step_index > 0 ? t.step_index : (idx + 1);
      const timeStr = new Date(t.timestamp).toLocaleTimeString();
      const cost = t.cost_usd !== null && t.cost_usd !== undefined ? '$' + t.cost_usd.toFixed(4) : '—';
      const latency = t.latency_ms ? (t.latency_ms / 1000).toFixed(2) + 's' : '—';

      let toolHtml = '—';
      if (t.tool_calls && t.tool_calls.length > 0) {
        toolHtml = t.tool_calls.map(tc => {
          const cls = tc.status === 'error' ? 'tag-error' : 'tag-ok';
          return `<span class="tool-tag ${cls}" title="${tc.tool_name} (${tc.status})">${tc.tool_name}</span>`;
        }).join('');
      } else if (t.tool_call_count > 0) {
        toolHtml = `${t.tool_call_count} call(s)`;
      }

      tr.innerHTML = `
        <td>${stepNum}</td>
        <td>${timeStr}</td>
        <td>${t.model || '—'}</td>
        <td>${formatTokens(t.input_tokens)}</td>
        <td>${formatTokens(t.cache_read_tokens)}</td>
        <td>${formatTokens(t.output_tokens)}</td>
        <td>${cost}</td>
        <td>${latency}</td>
        <td>${toolHtml}</td>
      `;
      turnsEl.appendChild(tr);
    });
  } catch (err) {
    summaryEl.innerHTML = `<div style="color: #f85149">Failed to load details: ${err.message}</div>`;
  }
}

function closeSessionModal() {
  document.getElementById('session-modal').style.display = 'none';
}

window.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    closeSessionModal();
  }
});

document.addEventListener('DOMContentLoaded', () => {
  loadDashboard();
  setInterval(loadDashboard, 60000);
});
