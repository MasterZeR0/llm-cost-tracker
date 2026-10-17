let charts = {};

async function fetchJSON(url) {
  const res = await fetch(url);
  return res.json();
}

function formatTokens(num) {
  if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
  if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
  return num.toString();
}

async function loadDashboard() {
  const period = document.getElementById('period').value;

  const overview = await fetchJSON(`/api/overview?period=${period}`);
  document.getElementById('total-requests').innerText = overview.total_requests.toLocaleString();
  document.getElementById('total-tokens').innerText = formatTokens(overview.total_tokens);
  document.getElementById('total-cost').innerText = '$' + overview.total_cost.toFixed(2);
  document.getElementById('avg-latency').innerText = (overview.avg_latency_ms / 1000).toFixed(2) + 's';

  loadCostOverTime(period);
  loadCostByModel(period);
  loadTokensByModel(period);
  loadToolUsage(period);
  loadRequests(period);
}

async function loadCostOverTime(period) {
  const data = await fetchJSON(`/api/cost-over-time?period=${period}`);
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
  const data = await fetchJSON(`/api/cost-by-model?period=${period}`);
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
  const data = await fetchJSON(`/api/tokens-by-model?period=${period}`);
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
  const data = await fetchJSON(`/api/tool-usage?period=${period}`);
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
      <td>${r.input_tokens.toLocaleString()}</td>
      <td>${r.output_tokens.toLocaleString()}</td>
      <td>${cost}</td>
      <td>${latency}</td>
    `;
    tbody.appendChild(tr);
  });
}

document.addEventListener('DOMContentLoaded', () => {
  loadDashboard();
  setInterval(loadDashboard, 60000);
});
