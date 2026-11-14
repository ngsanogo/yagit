import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { api, ApiError } from './client';

/**
 * The routes that cross a network answer with a stream: progress lines while
 * git runs, then one terminal event. Clone ends on the repository it made;
 * fetch, pull and push end on the references they moved. What is pinned here
 * is everything a person can see of that stream — which lines reach the
 * progress callback, what the call resolves to, and the exact sentence of
 * every way it can fail — for both shapes of the terminal event.
 */

const encoder = new TextEncoder();

/** A response whose body arrives in the given chunks, cut wherever they are cut. */
function streamed(chunks: string[], init: ResponseInit = {}): Response {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      controller.close();
    },
  });
  return new Response(body, {
    status: 200,
    headers: { 'Content-Type': 'application/x-ndjson; charset=utf-8' },
    ...init,
  });
}

function json(status: number, payload: unknown): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
  });
}

let reload: ReturnType<typeof vi.fn>;
let sent: { path: string; init: RequestInit }[];

function answer(response: Response | (() => Promise<Response>)) {
  vi.stubGlobal('fetch', (path: string, init: RequestInit) => {
    sent.push({ path, init });
    return typeof response === 'function' ? response() : Promise.resolve(response);
  });
}

beforeEach(() => {
  reload = vi.fn();
  sent = [];
  vi.stubGlobal('window', { location: { host: 'yagit.test', reload } });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const repository = { id: 'r1', path: '/root/copy', name: 'copy' };
const refs = { refs: [{ name: 'refs/heads/main' }], head: { name: 'main' } };

/** The two shapes, driven the same way. */
const routes = [
  {
    name: 'clone',
    path: '/api/repos/clone',
    run: (onProgress: (line: string) => void) =>
      api.clone('https://example.org/x.git', '/root/copy', onProgress),
    done: { type: 'done', repository },
    result: repository,
    failed: 'clone failed',
    noBody: 'the clone response had no body to read',
    noResult: 'clone ended without a repository',
  },
  {
    name: 'fetch',
    path: '/api/repos/r1/fetch',
    run: (onProgress: (line: string) => void) => api.fetchRemote('r1', 'origin', onProgress),
    done: { type: 'done', ...refs },
    result: refs,
    failed: '/api/repos/r1/fetch failed',
    noBody: '/api/repos/r1/fetch response had no body to read',
    noResult: '/api/repos/r1/fetch ended without a result',
  },
];

describe.each(routes)('$name streams its progress', (route) => {
  it('posts the body as JSON and asks for the stream', async () => {
    answer(streamed([`${JSON.stringify(route.done)}\n`]));
    await route.run(() => undefined);

    expect(sent).toHaveLength(1);
    expect(sent[0]?.path).toBe(route.path);
    expect(sent[0]?.init.method).toBe('POST');
    expect(sent[0]?.init.credentials).toBe('same-origin');
    expect(sent[0]?.init.headers).toEqual({
      Accept: 'application/x-ndjson, application/json',
      'Content-Type': 'application/json',
    });
  });

  it('hands every progress line over in order, whatever the chunks', async () => {
    const lines = [
      '{"type":"progress","line":"Counting objects: 1"}\n{"type":"pro',
      'gress","line":"Counting objects: 2"}\n\n',
      `${JSON.stringify(route.done)}`,
    ];
    answer(streamed(lines));
    const progress: string[] = [];

    await expect(route.run((line) => progress.push(line))).resolves.toEqual(route.result);
    expect(progress).toEqual(['Counting objects: 1', 'Counting objects: 2']);
  });

  it('fails with what git said when the stream ends on an error', async () => {
    const git = { command: 'git fetch', args: ['fetch'], exit_code: 128, stderr: 'fatal: no' };
    answer(
      streamed([`${JSON.stringify({ type: 'error', error: { message: 'it broke', git } })}\n`]),
    );

    const failure = await route.run(() => undefined).catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(ApiError);
    expect(failure).toMatchObject({ status: 200, message: 'it broke', git });
  });

  it('names the route when an error event carries no sentence', async () => {
    answer(streamed(['{"type":"error"}\n']));
    await expect(route.run(() => undefined)).rejects.toThrow(new ApiError(200, route.failed));
  });

  it('reads a refusal before the stream as an ordinary JSON error', async () => {
    answer(json(409, { error: { message: 'destination already exists' } }));
    const failure = await route.run(() => undefined).catch((error: unknown) => error);
    expect(failure).toMatchObject({ status: 409, message: 'destination already exists' });
  });

  it('falls back to the status and the route when the refusal is not JSON', async () => {
    answer(new Response('gateway down', { status: 502 }));
    await expect(route.run(() => undefined)).rejects.toThrow(`502 from ${route.path}`);
  });

  it('reloads on a session that ended', async () => {
    answer(new Response(null, { status: 401 }));
    await expect(route.run(() => undefined)).rejects.toThrow('the session expired');
    expect(reload).toHaveBeenCalledOnce();
  });

  it('says the daemon is not answering when the request never completes', async () => {
    answer(() => Promise.reject(new TypeError('Failed to fetch')));
    const failure = await route.run(() => undefined).catch((error: unknown) => error);
    expect(failure).toMatchObject({
      status: 0,
      message: 'the daemon is not answering on yagit.test',
    });
  });

  it('says how long git was silent when the idle deadline fires', async () => {
    answer(() => Promise.reject(new DOMException('idle', 'TimeoutError')));
    await expect(route.run(() => undefined)).rejects.toThrow(
      'git reported nothing for 5 minutes, so the transfer was given up on',
    );
  });

  it('refuses a stream with no body', async () => {
    answer(
      new Response(null, { status: 200, headers: { 'Content-Type': 'application/x-ndjson' } }),
    );
    await expect(route.run(() => undefined)).rejects.toThrow(route.noBody);
  });

  it('refuses a stream that ends without its terminal event', async () => {
    answer(streamed(['{"type":"progress","line":"Receiving objects"}\n']));
    await expect(route.run(() => undefined)).rejects.toThrow(route.noResult);
  });
});

it('drops the type of the terminal event from the references it carries', async () => {
  answer(streamed([`${JSON.stringify({ type: 'done', ...refs })}\n`]));
  const moved = await api.pull('r1', 'ff-only');
  expect(moved).toEqual(refs);
  expect(moved).not.toHaveProperty('type');
});

it('does not count a clone as finished on a terminal event with no repository', async () => {
  answer(streamed(['{"type":"done"}\n']));
  await expect(
    api.clone('https://example.org/x.git', '/root/copy', () => undefined),
  ).rejects.toThrow('clone ended without a repository');
});
