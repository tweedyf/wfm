// Modern WFM JavaScript

(function() {
  'use strict';

  // Initialize drag and drop
  function initDragDrop() {
    const form = document.getElementById('wfmForm');
    if (!form) return;

    const dirInput = form.querySelector('input[name="dir"]');
    const sortInput = form.querySelector('input[name="sort"]');
    if (!dirInput || !sortInput) return;

    function prevent(e) {
      e.preventDefault();
      e.stopPropagation();
    }

    function onDragOver(e) {
      prevent(e);
      if (e.dataTransfer.types.indexOf('Files') >= 0) {
        document.body.classList.add('drag-over');
      }
    }

    function onDragLeave(e) {
      prevent(e);
      if (!e.relatedTarget || !document.body.contains(e.relatedTarget)) {
        document.body.classList.remove('drag-over');
      }
    }

    function onDrop(e) {
      prevent(e);
      document.body.classList.remove('drag-over');
      const files = e.dataTransfer.files;
      if (!files || !files.length) return;

      const dir = dirInput.value;
      const sort = sortInput.value || '';
      const action = form.action;

      function upload(i) {
        if (i >= files.length) {
          if (i > 0) location.reload();
          return;
        }
        const fd = new FormData();
        fd.append('dir', dir);
        fd.append('sort', sort);
        fd.append('upload', '1');
        fd.append('filename', files[i]);
        fetch(action, {
          method: 'POST',
          body: fd,
          redirect: 'manual'
        }).then(function(r) {
          if (r.type === 'opaqueredirect' || (r.status >= 300 && r.status < 400)) {
            if (i + 1 >= files.length) location.reload();
            else upload(i + 1);
          } else {
            upload(i + 1);
          }
        }).catch(function() {
          upload(i + 1);
        });
      }
      upload(0);
    }

    document.addEventListener('dragover', onDragOver);
    document.addEventListener('dragenter', onDragOver);
    document.addEventListener('dragleave', onDragLeave);
    document.addEventListener('drop', onDrop);
  }

  // Initialize editor sync
  function initEditor() {
    const form = document.getElementById('wfmForm');
    const pre = document.getElementById('wfmEditor');
    const hidden = document.getElementById('wfmEditorInput');
    
    if (!form || !pre || !hidden) return;

    form.addEventListener('submit', function() {
      if (pre.innerText !== undefined) {
        hidden.value = pre.innerText;
      } else if (pre.textContent !== undefined) {
        hidden.value = pre.textContent;
      }
    });
  }

  // Initialize checkboxes for multi-select
  function initCheckboxes() {
    const checkboxes = document.querySelectorAll('input[name="mulf"]');
    const selectAllBtn = document.getElementById('selectAll');
    
    if (selectAllBtn) {
      selectAllBtn.addEventListener('click', function(e) {
        e.preventDefault();
        const allChecked = Array.from(checkboxes).every(cb => cb.checked);
        checkboxes.forEach(cb => {
          cb.checked = !allChecked;
        });
        updateSelectAllButton();
      });
    }

    checkboxes.forEach(cb => {
      cb.addEventListener('change', updateSelectAllButton);
    });

    function updateSelectAllButton() {
      if (selectAllBtn) {
        const allChecked = Array.from(checkboxes).every(cb => cb.checked);
        const someChecked = Array.from(checkboxes).some(cb => cb.checked);
        selectAllBtn.textContent = allChecked ? 'Deselect All' : 'Select All';
        selectAllBtn.style.display = someChecked || allChecked ? 'inline-block' : 'none';
      }
    }
  }

  // Initialize on DOM ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function() {
      initDragDrop();
      initEditor();
      initCheckboxes();
    });
  } else {
    initDragDrop();
    initEditor();
    initCheckboxes();
  }
})();
