CREATE TABLE IF NOT EXISTS categories (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL CHECK (name <> ''),
    sorting SMALLINT UNIQUE NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS municipalities (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL CHECK (name <> ''),
    sorting SMALLINT UNIQUE NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

INSERT INTO categories (sorting, name, is_active)
VALUES
    (1, 'Благоустройство (мусор, сорная растительность и др.)', true),
    (2, 'Фасады и ограждения (надписи, наклейки, внешний вид)', true),
    (3, 'Транспортные средства (размещение на озелененной территории, объектах благоустройства, грунте в МКД)', true),
    (4, 'Информационные конструкции (вывески, указатели, номерные знаки строений)', true),
    (5, 'Средства индивидуальной мобильности', true),
    (6, 'Домашние животные (выгул без поводка и намордника)', true),
    (7, 'Выпас сельскохозяйственных животных', true),
    (8, 'Продажа алкоголя в дни запрета', true),
    (9, 'Торговля вне установленных мест (нестационарные объекты)', true),
    (10, 'Тишина и покой в ночное время', true),
    (11, 'Нарушение запретов, установленных постановлением Губернатора Волгоградской области', true),
    (12, 'Иное', true)
ON CONFLICT (sorting) DO UPDATE
SET
    name = EXCLUDED.name,
    is_active = EXCLUDED.is_active;

INSERT INTO municipalities (sorting, name, is_active)
VALUES
    (1, 'г. Волгоград', true),
    (2, 'г. Волжский', true),
    (3, 'г. Камышин', true),
    (4, 'г. Михайловка', true),
    (5, 'г. Урюпинск', true),
    (6, 'г. Фролово', true),
    (7, 'Алексеевский район', true),
    (8, 'Быковский район', true),
    (9, 'Городищенский район', true),
    (10, 'Даниловский район', true),
    (11, 'Дубовской район', true),
    (12, 'Еланский район', true),
    (13, 'Жирновский район', true),
    (14, 'Иловлинский район', true),
    (15, 'Калачевский район', true),
    (16, 'Камышинский район', true),
    (17, 'Киквидзенский район', true),
    (18, 'Клетский район', true),
    (19, 'Котельниковский район', true),
    (20, 'Котовский район', true),
    (21, 'Кумылженский район', true),
    (22, 'Ленинский район', true),
    (23, 'Нехаевский район', true),
    (24, 'Николаевский район', true),
    (25, 'Новоаннинский район', true),
    (26, 'Новониколаевский район', true),
    (27, 'Октябрьский район', true),
    (28, 'Ольховский район', true),
    (29, 'Палласовский район', true),
    (30, 'Руднянский район', true),
    (31, 'Светлоярский район', true),
    (32, 'Серафимовичский район', true),
    (33, 'Среднеахтубинский район', true),
    (34, 'Старополтавский район', true),
    (35, 'Суровикинский район', true),
    (36, 'Урюпинский район', true),
    (37, 'Фроловский район', true),
    (38, 'Чернышковский район', true)
ON CONFLICT (sorting) DO UPDATE
SET
    name = EXCLUDED.name,
    is_active = EXCLUDED.is_active;
