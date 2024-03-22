/**
 * @param {HTMLElement} element
 */
function copyContent(element) {
    navigator.clipboard.writeText(element.innerText);

    let oldTooltip = element.attributes["data-tooltip"].value;
    element.attributes["data-tooltip"].value = "Copied!";
    setTimeout(() => { element.attributes["data-tooltip"].value = oldTooltip }, 5000);
}