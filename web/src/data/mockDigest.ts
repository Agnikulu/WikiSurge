import type { DigestData } from '../types/digest';

const now = new Date();
const yesterday = new Date(now.getTime() - 24 * 60 * 60 * 1000);
const weekAgo = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);

export const mockDailyDigest: DigestData = {
  period: 'daily',
  period_start: yesterday.toISOString(),
  period_end: now.toISOString(),
  edit_war_highlights: [
    {
      rank: 1,
      title: '2025 Turkish earthquake',
      edit_count: 847,
      event_type: 'edit_war',
      summary:
        'Massive revert war over casualty figures — 23 editors involved',
      editor_count: 23,
      editors: [
        'GeoTracker99',
        'SeismicFacts',
        'TurkWatcher',
        'ReliefNow',
        'FactCheck2025',
      ],
      revert_count: 34,
      severity: 'critical',
      llm_summary:
        "A fierce editorial conflict has erupted over the reported casualty figures, with two opposing camps: one citing Turkish government sources and the other relying on international relief organization estimates. Editors are repeatedly reverting each other's numbers, with the dispute centering on whether to use 'confirmed' vs 'estimated' death tolls.",
      content_area: 'Casualty figures and sourcing',
    },
    {
      rank: 2,
      title: 'Pope Francis',
      edit_count: 312,
      event_type: 'edit_war',
      summary:
        'Edit war over legacy section after Vatican announcement',
      editor_count: 8,
      editors: ['VaticanWatch', 'CatholicEdit', 'HistoryBuff42'],
      revert_count: 12,
      severity: 'high',
      llm_summary:
        "Disagreement over how to characterize Pope Francis's legacy following a major Vatican announcement. Conservative and progressive editors are clashing over the framing of his papacy's impact on church doctrine.",
      content_area: 'Legacy and doctrinal impact',
    },
    {
      rank: 3,
      title: 'OpenAI',
      edit_count: 589,
      event_type: 'edit_war',
      summary:
        'Conflicting edits on board restructuring details',
      editor_count: 15,
      editors: ['TechInsider', 'AIWatcher', 'BoardroomLeaks', 'NeutralPOV'],
      revert_count: 22,
      severity: 'high',
      llm_summary:
        "Multiple editors are disputing the characterization of OpenAI's board restructuring, with disagreements over whether certain executive departures were voluntary or forced. Sources from competing news outlets are being used to support opposing narratives.",
      content_area: 'Corporate governance',
    },
    {
      rank: 4,
      title: 'Boeing 737 MAX',
      edit_count: 156,
      event_type: 'edit_war',
      summary: 'Dispute over safety record phrasing',
      editor_count: 6,
      editors: ['AviationPro', 'SafetyFirst', 'BoeingFan'],
      revert_count: 8,
      severity: 'moderate',
      llm_summary:
        "Ongoing dispute about how to phrase the safety record section, particularly around whether recent incidents should be characterized as 'design flaws' or 'operational issues'.",
      content_area: 'Safety record',
    },
  ],
  trending_highlights: [
    {
      rank: 1,
      title: 'Taylor Swift',
      edit_count: 204,
      event_type: 'trending',
      summary:
        'Trending after surprise album drop — discography updates',
    },
    {
      rank: 2,
      title: 'Mars Rover Curiosity',
      edit_count: 89,
      event_type: 'trending',
      summary:
        'Trending (score: 1250) — new mineral discovery announcement',
    },
    {
      rank: 3,
      title: '2026 FIFA World Cup',
      edit_count: 156,
      event_type: 'trending',
      summary: 'Trending (score: 980) — host city updates',
    },
  ],
  global_highlights: [],
  watchlist_events: [
    {
      title: 'Bitcoin',
      edit_count: 267,
      is_notable: true,
      spike_ratio: 4.2,
      event_type: 'edit_war',
      summary: 'Edit war over ETF price impact section',
    },
    {
      title: 'Ethereum',
      edit_count: 89,
      is_notable: true,
      spike_ratio: 2.1,
      event_type: 'trending',
      summary: 'Trending — merge anniversary edits',
    },
    {
      title: 'Solana',
      edit_count: 12,
      is_notable: false,
      event_type: 'active',
      summary: 'Minor copyedits',
    },
    {
      title: 'Cardano',
      edit_count: 3,
      is_notable: false,
      event_type: 'quiet',
      summary: 'Quiet — no significant changes',
    },
  ],
  stats: {
    total_edits: 2_487_319,
    edit_wars: 17,
    top_languages: [
      { language: 'en', count: 892000, percentage: 35.9 },
      { language: 'de', count: 412000, percentage: 16.6 },
      { language: 'ja', count: 298000, percentage: 12.0 },
      { language: 'es', count: 245000, percentage: 9.8 },
      { language: 'fr', count: 198000, percentage: 8.0 },
    ],
  },
};

export const mockWeeklyDigest: DigestData = {
  period: 'weekly',
  period_start: weekAgo.toISOString(),
  period_end: now.toISOString(),
  edit_war_highlights: [
    {
      rank: 1,
      title: '2025 Turkish earthquake',
      edit_count: 3412,
      event_type: 'edit_war',
      summary:
        'Week-long revert war over casualty figures — 47 unique editors',
      editor_count: 47,
      editors: [
        'GeoTracker99',
        'SeismicFacts',
        'TurkWatcher',
        'ReliefNow',
        'FactCheck2025',
        'DisasterWatch',
        'TurkGov_Source',
      ],
      revert_count: 112,
      severity: 'critical',
      llm_summary:
        "The week-long editorial conflict over casualty figures intensified as official Turkish government numbers diverged further from international relief estimates. A mediation request has been filed but not yet acted upon. The article is currently semi-protected.",
      content_area: 'Casualty figures, relief efforts, and government response',
    },
    {
      rank: 2,
      title: 'Artificial intelligence',
      edit_count: 1823,
      event_type: 'edit_war',
      summary:
        'Major restructuring dispute over AI safety section',
      editor_count: 31,
      editors: ['AIEthics101', 'TechOptimist', 'SafeAI', 'DeepMindFan', 'AISkeptic'],
      revert_count: 67,
      severity: 'high',
      llm_summary:
        "A prolonged dispute over the AI safety section has emerged between those who want to emphasize existential risk and those who argue the section is disproportionate. Multiple reliable sources are being cited by both sides, and the talk page discussion has exceeded 50,000 words this week.",
      content_area: 'AI safety and existential risk',
    },
    {
      rank: 3,
      title: 'Russia–Ukraine war',
      edit_count: 2156,
      event_type: 'edit_war',
      summary:
        'Ongoing neutrality disputes over territorial claims',
      editor_count: 38,
      editors: ['UkraineFirst', 'NeutralObserver', 'GeoPolAnalyst'],
      revert_count: 89,
      severity: 'high',
      llm_summary:
        "Persistent editorial conflicts over how to describe territorial changes. Key disputes include the use of 'occupied' vs 'controlled' terminology and whether to include certain maps that one side considers propaganda.",
      content_area: 'Territorial claims and neutrality',
    },
    {
      rank: 4,
      title: 'COVID-19 pandemic',
      edit_count: 678,
      event_type: 'edit_war',
      summary:
        'Lab leak theory phrasing dispute resurfaces',
      editor_count: 14,
      editors: ['ViralOrigins', 'ScienceFirst', 'LabLeakTruth'],
      revert_count: 23,
      severity: 'moderate',
      llm_summary:
        "The recurring dispute over how to characterize the lab leak hypothesis has resurfaced following new intelligence reports. Editors disagree on whether to upgrade the theory from 'hypothesis' to 'leading theory'.",
      content_area: 'Origins and lab leak hypothesis',
    },
    {
      rank: 5,
      title: 'Donald Trump',
      edit_count: 1245,
      event_type: 'edit_war',
      summary: 'Legal section edits contested after court ruling',
      editor_count: 22,
      editors: ['PolitiFact', 'MAGA2025', 'NeutralPOV', 'LegalEagle'],
      revert_count: 45,
      severity: 'high',
      llm_summary:
        "Following a major court ruling, editors are clashing over how to characterize the legal proceedings and their political implications. Disputes center on the use of 'conviction' vs 'ruling' and whether editorial commentary from major newspapers should be included in the lead section.",
      content_area: 'Legal proceedings and political impact',
    },
  ],
  trending_highlights: [
    {
      rank: 1,
      title: 'Taylor Swift',
      edit_count: 1456,
      event_type: 'trending',
      summary:
        'Dominated trending all week — new album, tour dates, award nominations',
    },
    {
      rank: 2,
      title: 'Mars Rover Curiosity',
      edit_count: 567,
      event_type: 'trending',
      summary:
        'Sustained interest after organic molecule discovery confirmed',
    },
    {
      rank: 3,
      title: '2026 FIFA World Cup',
      edit_count: 923,
      event_type: 'trending',
      summary:
        'Qualification results and venue updates throughout the week',
    },
    {
      rank: 4,
      title: 'Nvidia',
      edit_count: 445,
      event_type: 'trending',
      summary: 'Stock price ATH and new GPU architecture announcement',
    },
    {
      rank: 5,
      title: 'Indian general election, 2026',
      edit_count: 812,
      event_type: 'trending',
      summary: 'Early polling data and candidate announcements driving edits',
    },
  ],
  global_highlights: [],
  watchlist_events: [
    {
      title: 'Bitcoin',
      edit_count: 1834,
      is_notable: true,
      spike_ratio: 5.7,
      event_type: 'edit_war',
      summary:
        'Week-long edit war over ETF impact, price predictions, and regulatory status',
    },
    {
      title: 'Ethereum',
      edit_count: 456,
      is_notable: true,
      spike_ratio: 3.2,
      event_type: 'trending',
      summary:
        'Trending all week — Layer 2 updates and staking yield changes',
    },
    {
      title: 'Solana',
      edit_count: 89,
      is_notable: true,
      spike_ratio: 2.4,
      event_type: 'trending',
      summary: 'Notable activity — DeFi ecosystem growth coverage',
    },
    {
      title: 'Cardano',
      edit_count: 15,
      is_notable: false,
      event_type: 'quiet',
      summary: 'Quiet week — routine citation updates',
    },
  ],
  stats: {
    total_edits: 17_420_000,
    edit_wars: 94,
    top_languages: [
      { language: 'en', count: 6_250_000, percentage: 35.9 },
      { language: 'de', count: 2_890_000, percentage: 16.6 },
      { language: 'ja', count: 2_090_000, percentage: 12.0 },
      { language: 'es', count: 1_707_000, percentage: 9.8 },
      { language: 'fr', count: 1_394_000, percentage: 8.0 },
    ],
  },
};
