-- +goose Up
CREATE TABLE budgets (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category VARCHAR(100) NOT NULL CHECK (length(btrim(category)) > 0),
    limit_amount NUMERIC(14, 2) NOT NULL CHECK (limit_amount > 0),
    PRIMARY KEY (user_id, category)
);
CREATE TABLE expenses (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount NUMERIC(14, 2) NOT NULL CHECK (amount > 0),
    category VARCHAR(100) NOT NULL CHECK (length(btrim(category)) > 0),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    date DATE NOT NULL
);
CREATE INDEX expenses_user_category_idx ON expenses(user_id, category);
CREATE INDEX expenses_user_date_idx ON expenses(user_id, date);

-- +goose Down
DROP TABLE expenses;
DROP TABLE budgets;
