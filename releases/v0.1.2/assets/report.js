// Progressive enhancement: every result remains readable without JavaScript.
const filters = document.querySelector("[data-filters]");
if (filters) {
  filters.hidden = false;
  const search = filters.querySelector("[data-search]");
  const status = filters.querySelector("[data-status]");
  const category = filters.querySelector("[data-category]");
  const counter = filters.querySelector("[data-count]");
  const rows = [...document.querySelectorAll("[data-result]")].map(
    (element) => ({ element, text: element.textContent.toLowerCase() }),
  );
  const apply = () => {
    let visible = 0;
    for (const { element, text } of rows) {
      element.hidden = !(
        text.includes(search.value.toLowerCase()) &&
        (!status.value || element.dataset.status === status.value) &&
        (!category?.value || element.dataset.category === category.value)
      );
      if (!element.hidden) visible++;
    }
    counter.textContent = `${visible} of ${rows.length} shown`;
  };
  filters.addEventListener("input", apply);
  apply();
  const reveal = () => {
    const target = document.getElementById(location.hash.slice(1));
    if (target?.matches("details[data-result]")) {
      search.value = "";
      status.value = "";
      if (category) category.value = "";
      apply();
      target.open = true;
      target.scrollIntoView();
    }
  };
  window.addEventListener("hashchange", reveal);
  reveal();
}
