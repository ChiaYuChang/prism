BEGIN;

INSERT INTO sources (abbr, name, type, base_url) VALUES
    ('dpp',   '民主進步黨',     'PARTY', 'https://www.dpp.org.tw'),
    ('tpp',   '台灣民眾黨',     'PARTY', 'https://www.tpp.org.tw'),
    ('kmt',   '中國國民黨',     'PARTY', 'https://www.kmt.org.tw'),
    ('cna',   '中央通訊社',     'MEDIA', 'https://www.cna.com.tw'),
    ('pts',   '公共電視台',     'MEDIA', 'https://news.pts.org.tw'),
    ('ttv',   '台灣電視公司',   'MEDIA', 'https://www.ttv.com.tw'),
    ('yahoo', 'Yahoo奇摩新聞', 'MEDIA', 'https://tw.news.yahoo.com'),
    ('brave', 'Brave Search',  'MEDIA', 'https://api.search.brave.com')
ON CONFLICT (abbr) DO NOTHING;

COMMIT;
