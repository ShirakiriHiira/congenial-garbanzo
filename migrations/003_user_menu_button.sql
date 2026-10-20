-- Mini App menu button already installed for this private chat. NULL means the
-- bot has not installed one yet; a stored URL different from the configured one
-- triggers a single re-install.
ALTER TABLE users ADD COLUMN menu_button_url TEXT;
