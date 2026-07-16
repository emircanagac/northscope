import { expect, test, type Page } from '@playwright/test';

const snapshot = {
  version: 1,
  generatedAt: '2026-07-16T12:00:00Z',
  inventory: {
    controllers: 1,
    ingresses: 1,
    services: 12,
    pods: 36,
    nodes: 5,
  },
  nodes: [
    {
      id: 'controller:nginx',
      type: 'northscopeNode',
      position: { x: 0, y: 0 },
      data: {
        label: 'nginx',
        kind: 'Controller',
        name: 'nginx',
        properties: { controller: 'k8s.io/ingress-nginx' },
      },
    },
    {
      id: 'ingress:demo:store',
      type: 'northscopeNode',
      position: { x: 0, y: 0 },
      data: {
        label: 'demo/store',
        kind: 'Ingress',
        namespace: 'demo',
        name: 'store',
        status: 'Configured',
        properties: { className: 'nginx', hosts: 'shop.example.com' },
      },
    },
    {
      id: 'route:demo:store:root',
      type: 'northscopeNode',
      position: { x: 0, y: 0 },
      data: {
        label: 'shop.example.com /',
        kind: 'Route',
        namespace: 'demo',
        name: 'shop.example.com /',
        status: 'Healthy',
        properties: {
          host: 'shop.example.com',
          path: '/',
          backend: 'frontend',
          servicePort: 'http',
          ingressName: 'store',
          severity: 'ok',
        },
      },
    },
    {
      id: 'service:demo:frontend',
      type: 'northscopeNode',
      position: { x: 0, y: 0 },
      data: {
        label: 'demo/frontend',
        kind: 'Service',
        namespace: 'demo',
        name: 'frontend',
        status: 'Active',
        properties: { ports: '80/TCP' },
      },
    },
    {
      id: 'pod:demo:frontend-6f46558fcf-very-long-pod-name',
      type: 'northscopeNode',
      position: { x: 0, y: 0 },
      data: {
        label: 'demo/frontend-6f46558fcf-very-long-pod-name',
        kind: 'Pod',
        namespace: 'demo',
        name: 'frontend-6f46558fcf-very-long-pod-name',
        status: 'ContainersNotReadyWithAnIntentionallyLongDiagnosticReason',
        properties: { podIP: '10.42.0.18', nodeName: 'worker-with-a-long-name' },
      },
    },
    {
      id: 'node::worker-with-a-long-name',
      type: 'northscopeNode',
      position: { x: 0, y: 0 },
      data: {
        label: 'worker-with-a-long-name',
        kind: 'Node',
        name: 'worker-with-a-long-name',
        status: 'Ready=True',
      },
    },
  ],
  edges: [
    { id: 'controller-ingress', source: 'controller:nginx', target: 'ingress:demo:store', data: { kind: 'controls' } },
    { id: 'ingress-route', source: 'ingress:demo:store', target: 'route:demo:store:root', data: { kind: 'defines' } },
    { id: 'route-service', source: 'route:demo:store:root', target: 'service:demo:frontend', data: { kind: 'routes' } },
    {
      id: 'service-pod',
      source: 'service:demo:frontend',
      target: 'pod:demo:frontend-6f46558fcf-very-long-pod-name',
      data: { kind: 'selects' },
    },
    {
      id: 'node-pod',
      source: 'node::worker-with-a-long-name',
      target: 'pod:demo:frontend-6f46558fcf-very-long-pod-name',
      data: { kind: 'hosts' },
    },
  ],
};

async function installFakeTopologyStream(page: Page): Promise<void> {
  await page.addInitScript((topologySnapshot) => {
    type SocketHandler = ((event: Event) => void) | null;
    type MessageHandler = ((event: MessageEvent) => void) | null;

    class FakeWebSocket {
      static CONNECTING = 0;
      static OPEN = 1;
      static CLOSING = 2;
      static CLOSED = 3;

      readyState = FakeWebSocket.CONNECTING;
      onopen: SocketHandler = null;
      onmessage: MessageHandler = null;
      onerror: SocketHandler = null;
      onclose: SocketHandler = null;

      constructor() {
        const control = window.__northscopeSocketControl;
        control.instances.push(this);
        window.setTimeout(() => {
          this.readyState = FakeWebSocket.OPEN;
          this.onopen?.(new Event('open'));
          this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(topologySnapshot) }));
        }, 10);
      }

      close() {
        if (this.readyState === FakeWebSocket.CLOSED) {
          return;
        }
        this.readyState = FakeWebSocket.CLOSED;
        this.onclose?.(new Event('close'));
      }

      send() {}
    }

    window.__northscopeSocketControl = {
      instances: [] as FakeWebSocket[],
      closeLatest() {
        this.instances.at(-1)?.close();
      },
    };
    Object.assign(window, { WebSocket: FakeWebSocket });
  }, snapshot);
}

declare global {
  interface Window {
    __northscopeSocketControl: {
      instances: Array<{ close(): void }>;
      closeLatest(): void;
    };
  }
}

async function openSelectedTopology(page: Page): Promise<void> {
  await installFakeTopologyStream(page);
  await page.goto('/');
  await expect(page.getByTestId('stream-status')).toHaveText('Live config');
  await page.getByTestId('host-route').filter({ hasText: 'shop.example.com' }).click();
  await expect(page.getByTestId('topology-node-card').first()).toBeVisible();
}

test('dark theme persists across reloads', async ({ page }) => {
  await openSelectedTopology(page);

  await page.getByRole('button', { name: 'Switch to dark theme' }).click();
  await expect(page.locator('html')).toHaveClass(/dark/);

  await page.reload();
  await expect(page.locator('html')).toHaveClass(/dark/);
  await expect(page.getByRole('button', { name: 'Switch to light theme' })).toBeVisible();
});

test('websocket reconnect restores live topology', async ({ page }) => {
  await openSelectedTopology(page);

  await page.evaluate(() => window.__northscopeSocketControl.closeLatest());
  await expect(page.getByTestId('stream-status')).toHaveText('Reconnecting');
  await expect(page.getByTestId('stream-status')).toHaveText('Live config', { timeout: 3_000 });
  await expect.poll(() => page.evaluate(() => window.__northscopeSocketControl.instances.length)).toBeGreaterThan(1);
});

test('pan and zoom remain stable without viewport reset', async ({ page }) => {
  await openSelectedTopology(page);

  const pane = page.locator('.react-flow__pane');
  const viewport = page.locator('.react-flow__viewport');
  const initialTransform = await viewport.getAttribute('style');
  const box = await pane.boundingBox();
  if (!box) {
    throw new Error('React Flow pane has no bounding box');
  }

  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.wheel(0, -500);
  await expect.poll(() => viewport.getAttribute('style')).not.toBe(initialTransform);
  const zoomedTransform = await viewport.getAttribute('style');

  await page.waitForTimeout(500);
  await expect(viewport).toHaveAttribute('style', zoomedTransform ?? '');

  await page.mouse.move(box.x + box.width * 0.65, box.y + box.height * 0.65);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * 0.5, box.y + box.height * 0.5, { steps: 6 });
  await page.mouse.up();
  await expect.poll(() => viewport.getAttribute('style')).not.toBe(zoomedTransform);
});

test('simple and expanded cards share dimensions and contain their content', async ({ page }) => {
  await openSelectedTopology(page);

  const simpleIngress = page.locator('[data-kind="Ingress"]').first();
  const simpleSize = await simpleIngress.evaluate((element) => {
    const style = window.getComputedStyle(element);
    return { width: style.width, height: style.height };
  });

  await page.getByRole('button', { name: 'Expanded' }).click();
  await expect(page.getByRole('button', { name: 'Expanded' })).toHaveAttribute('aria-pressed', 'true');
  const expandedIngress = page.locator('[data-kind="Ingress"]').first();
  const expandedSize = await expandedIngress.evaluate((element) => {
    const style = window.getComputedStyle(element);
    return { width: style.width, height: style.height };
  });

  expect(simpleSize).toEqual(expandedSize);
  await expect(page.locator('[data-kind="Pod"]').first()).toBeVisible();

  const overflowCount = await page.getByTestId('topology-node-card').evaluateAll((cards) =>
    cards.filter((card) => {
      const bounds = card.getBoundingClientRect();
      return Array.from(card.querySelectorAll<HTMLElement>('*')).some((child) => {
        if (child.classList.contains('react-flow__handle')) {
          return false;
        }
        const childBounds = child.getBoundingClientRect();
        return childBounds.left < bounds.left - 1 || childBounds.right > bounds.right + 1 || childBounds.top < bounds.top - 1 || childBounds.bottom > bounds.bottom + 1;
      });
    }).length,
  );
  expect(overflowCount).toBe(0);
});
