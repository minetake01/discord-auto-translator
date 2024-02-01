use deepl::{Lang, DeepLApi, Error};
use regex::Regex;

pub async fn translate_text(text: String, source_lang: &Lang, target_lang: &Lang, token: &str) -> Result<String, Error> {
    let mut text = text;
    let elements = convert_placeholder(&mut text);

    let deepl = DeepLApi::with(token).new();
	let translated_text = &mut deepl
		.translate_text(text, target_lang.clone())
		.source_lang(source_lang.clone())
		.await?
		.translations[0]
		.text;
    
    revert_placeholder(translated_text, elements);
	Ok(translated_text.to_string())
}

fn convert_placeholder(text: &mut String) -> Vec<String> {
    // URLをタグに置換
    let url_re = Regex::new(r"((https?://)[^\s]+)").unwrap();
    let elements: Vec<_> = url_re.find_iter(text).map(|mat| mat.as_str().to_string()).collect();
    for (index, element) in elements.iter().enumerate() {
        let tag = format!("<p i=\"{}\"></p>", index);
        *text = text.replacen(element, &tag, 1);
    }
    elements
}

fn revert_placeholder(text: &mut String, urls: Vec<String>) {
    // タグをURLに置換
    for (index, url) in urls.iter().enumerate() {
        let tag = format!("<u id=\"{}\"></u>", index);
        *text = text.replacen(&tag, url, 1);
    }
}