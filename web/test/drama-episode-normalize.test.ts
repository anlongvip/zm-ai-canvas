import { describe, expect, test } from "bun:test";

import { normalizeDramaEpisodeDetail } from "../src/services/api/drama";

describe("normalizeDramaEpisodeDetail", () => {
    test("repairs null collections from historical episode responses", () => {
        const detail = normalizeDramaEpisodeDetail({
            episode: { id: "episode-1" } as never,
            shots: null as never,
            segments: null as never,
            chapters: null as never,
            jobs: null as never,
        });

        expect(detail.shots).toEqual([]);
        expect(detail.segments).toEqual([]);
        expect(detail.chapters).toEqual([]);
        expect(detail.jobs).toEqual([]);
    });

    test("repairs null segment shot IDs without changing segment data", () => {
        const segment = { id: "segment-1", shotIds: null };
        const detail = normalizeDramaEpisodeDetail({
            episode: { id: "episode-1" } as never,
            shots: [],
            segments: [segment as never],
            chapters: [],
            jobs: [],
        });

        expect(detail.segments[0]).toMatchObject({ id: "segment-1", shotIds: [] });
    });
});
