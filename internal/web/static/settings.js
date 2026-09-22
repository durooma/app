(() => {
    const selector = document.getElementById('ai-model');
    const field = document.getElementById('custom-model-field');
    const input = document.getElementById('ai-custom-model');
    function updateCustomModel() {
        const custom = selector.value === 'custom';
        field.hidden = !custom;
        input.disabled = !custom;
        input.required = custom;
    }
    selector.addEventListener('change', updateCustomModel);
    updateCustomModel();
})();
