-- Default categories to get started. All are editable in the UI.
INSERT INTO categories (name, description, kind, sort_order) VALUES
    ('Salary',        'Employment income and wages',                    'income',  10),
    ('Dividend',      'Dividend payments from investments',             'income',  20),
    ('Stock Grant',   'Vested equity / RSU grants',                     'income',  30),
    ('Investment-Sell','Proceeds and gains from selling investments',   'income',  40),
    ('Other Income',  'Refunds, gifts, and miscellaneous income',       'income',  50),
    ('Housing',       'Rent, mortgage, and related housing costs',      'expense', 110),
    ('Utilities',     'Electricity, water, internet, phone',            'expense', 120),
    ('Groceries',     'Supermarket and food shopping',                  'expense', 130),
    ('Dining',        'Restaurants, cafes, and takeout',                'expense', 140),
    ('Transport',     'Public transit, fuel, car costs',                'expense', 150),
    ('Health',        'Insurance, doctors, pharmacy',                   'expense', 160),
    ('Shopping',      'Clothing, electronics, general retail',          'expense', 170),
    ('Travel',        'Flights, hotels, holidays',                      'expense', 180),
    ('Entertainment', 'Subscriptions, hobbies, leisure',               'expense', 190),
    ('Tax',           'Income tax and withholdings',                    'expense', 200),
    ('Fees',          'Bank and service fees',                          'expense', 210),
    ('General',       'Uncategorized fallback',                         NULL,      900)
ON CONFLICT (name) DO NOTHING;

INSERT INTO settings (key, value) VALUES ('base_currency', 'CHF')
ON CONFLICT (key) DO NOTHING;
